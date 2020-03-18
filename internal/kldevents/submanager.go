// Copyright 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldevents

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldkvstore"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
	"github.com/syndtr/goleveldb/leveldb"
)

const (
	// SubPathPrefix is the path prefix for subscriptions
	SubPathPrefix = "/subscriptions"
	// StreamPathPrefix is the path prefix for event streams
	StreamPathPrefix   = "/eventstreams"
	subIDPrefix        = "sb-"
	streamIDPrefix     = "es-"
	checkpointIDPrefix = "cp-"
)

// SubscriptionManager provides REST APIs for managing events
type SubscriptionManager interface {
	Init() error
	AddStream(ctx context.Context, spec *StreamInfo) (*StreamInfo, error)
	Streams(ctx context.Context) []*StreamInfo
	StreamByID(ctx context.Context, id string) (*StreamInfo, error)
	SuspendStream(ctx context.Context, id string) error
	ResumeStream(ctx context.Context, id string) error
	DeleteStream(ctx context.Context, id string) error
	AddSubscription(ctx context.Context, addr *kldbind.Address, event *kldbind.ABIEvent, streamID, initialBlock string) (*SubscriptionInfo, error)
	Subscriptions(ctx context.Context) []*SubscriptionInfo
	SubscriptionByID(ctx context.Context, id string) (*SubscriptionInfo, error)
	DeleteSubscription(ctx context.Context, id string) error
	Close()
}

type subscriptionManager interface {
	config() *SubscriptionManagerConf
	streamByID(string) (*eventStream, error)
	subscriptionByID(string) (*subscription, error)
	subscriptionsForStream(string) []*subscription
	loadCheckpoint(string) (map[string]*big.Int, error)
	storeCheckpoint(string, map[string]*big.Int) error
}

// SubscriptionManagerConf configuration
type SubscriptionManagerConf struct {
	EventLevelDBPath        string `json:"eventsDB"`
	EventPollingIntervalSec uint64 `json:"eventPollingIntervalSec,omitempty"`
	WebhooksAllowPrivateIPs bool   `json:"webhooksAllowPrivateIPs,omitempty"`
}

type subscriptionMGR struct {
	conf          *SubscriptionManagerConf
	rpcConf       *kldeth.RPCConnOpts
	db            kldkvstore.KVStore
	rpc           kldeth.RPCClient
	subscriptions map[string]*subscription
	streams       map[string]*eventStream
	closed        bool
}

// CobraInitSubscriptionManager standard naming for cobra command params
func CobraInitSubscriptionManager(cmd *cobra.Command, conf *SubscriptionManagerConf) {
	cmd.Flags().StringVarP(&conf.EventLevelDBPath, "events-db", "E", "", "Level DB location for subscription management")
	cmd.Flags().Uint64VarP(&conf.EventPollingIntervalSec, "events-polling-int", "j", 10, "Event polling interval (ms)")
	cmd.Flags().BoolVarP(&conf.WebhooksAllowPrivateIPs, "events-privips", "J", false, "Allow private IPs in Webhooks")
}

// NewSubscriptionManager construtor
func NewSubscriptionManager(conf *SubscriptionManagerConf, rpc kldeth.RPCClient) SubscriptionManager {
	sm := &subscriptionMGR{
		conf:          conf,
		rpc:           rpc,
		subscriptions: make(map[string]*subscription),
		streams:       make(map[string]*eventStream),
	}
	if conf.EventPollingIntervalSec <= 0 {
		conf.EventPollingIntervalSec = 1
	}
	return sm
}

// SubscriptionByID used externally to get serializable details
func (s *subscriptionMGR) SubscriptionByID(ctx context.Context, id string) (*SubscriptionInfo, error) {
	sub, err := s.subscriptionByID(id)
	if err != nil {
		return nil, err
	}
	return sub.info, err
}

// Subscriptions used externally to get list subscriptions
func (s *subscriptionMGR) Subscriptions(ctx context.Context) []*SubscriptionInfo {
	l := make([]*SubscriptionInfo, 0, len(s.subscriptions))
	for _, sub := range s.subscriptions {
		l = append(l, sub.info)
	}
	return l
}

// AddSubscription adds a new subscription
func (s *subscriptionMGR) AddSubscription(ctx context.Context, addr *kldbind.Address, event *kldbind.ABIEvent, streamID, initialBlock string) (*SubscriptionInfo, error) {
	i := &SubscriptionInfo{
		TimeSorted: kldmessages.TimeSorted{
			CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
		},
		ID:     subIDPrefix + kldutils.UUIDv4(),
		Event:  kldbind.MarshalledABIEvent{E: *event},
		Stream: streamID,
	}
	i.Path = SubPathPrefix + "/" + i.ID
	// Check initial block number to subscribe from
	if initialBlock == "" || initialBlock == FromBlockLatest {
		i.FromBlock = FromBlockLatest
	} else {
		var bi big.Int
		if _, ok := bi.SetString(initialBlock, 0); !ok {
			return nil, klderrors.Errorf(klderrors.EventStreamsSubscribeBadBlock)
		}
		i.FromBlock = bi.Text(10)
	}
	// Create it
	sub, err := newSubscription(s, s.rpc, addr, i)
	if err != nil {
		return nil, err
	}
	s.subscriptions[sub.info.ID] = sub
	return s.storeSubscription(sub.info)
}

func (s *subscriptionMGR) config() *SubscriptionManagerConf {
	return s.conf
}

// DeleteSubscription deletes a streamm
func (s *subscriptionMGR) DeleteSubscription(ctx context.Context, id string) error {
	sub, err := s.subscriptionByID(id)
	if err != nil {
		return err
	}
	return s.deleteSubscription(ctx, sub)
}

func (s *subscriptionMGR) deleteSubscription(ctx context.Context, sub *subscription) error {
	delete(s.subscriptions, sub.info.ID)
	sub.unsubscribe(ctx)
	if err := s.db.Delete(sub.info.ID); err != nil {
		return err
	}
	return nil
}

func (s *subscriptionMGR) storeSubscription(info *SubscriptionInfo) (*SubscriptionInfo, error) {
	infoBytes, _ := json.MarshalIndent(info, "", "  ")
	if err := s.db.Put(info.ID, infoBytes); err != nil {
		return nil, klderrors.Errorf(klderrors.EventStreamsSubscribeStoreFailed, err)
	}
	return info, nil
}

// StreamByID used externally to get serializable details
func (s *subscriptionMGR) StreamByID(ctx context.Context, id string) (*StreamInfo, error) {
	stream, err := s.streamByID(id)
	if err != nil {
		return nil, err
	}
	return stream.spec, nil
}

// Streams used externally to get list streams
func (s *subscriptionMGR) Streams(ctx context.Context) []*StreamInfo {
	l := make([]*StreamInfo, 0, len(s.subscriptions))
	for _, stream := range s.streams {
		l = append(l, stream.spec)
	}
	return l
}

// AddStream adds a new stream
func (s *subscriptionMGR) AddStream(ctx context.Context, spec *StreamInfo) (*StreamInfo, error) {
	spec.ID = streamIDPrefix + kldutils.UUIDv4()
	spec.CreatedISO8601 = time.Now().UTC().Format(time.RFC3339)
	spec.Path = StreamPathPrefix + "/" + spec.ID
	stream, err := newEventStream(s, spec)
	if err != nil {
		return nil, err
	}
	s.streams[stream.spec.ID] = stream
	return s.storeStream(stream.spec)
}

func (s *subscriptionMGR) storeStream(spec *StreamInfo) (*StreamInfo, error) {
	infoBytes, _ := json.MarshalIndent(spec, "", "  ")
	if err := s.db.Put(spec.ID, infoBytes); err != nil {
		return nil, klderrors.Errorf(klderrors.EventStreamsCreateStreamStoreFailed, err)
	}
	return spec, nil
}

// DeleteStream deletes a streamm
func (s *subscriptionMGR) DeleteStream(ctx context.Context, id string) error {
	stream, err := s.streamByID(id)
	if err != nil {
		return err
	}
	// We have to clean up all the associated subs
	for _, sub := range s.subscriptions {
		if sub.info.Stream == stream.spec.ID {
			s.deleteSubscription(ctx, sub)
		}
	}
	delete(s.streams, stream.spec.ID)
	stream.stop()
	if err = s.db.Delete(stream.spec.ID); err != nil {
		return err
	}
	s.deleteCheckpoint(stream.spec.ID)
	return nil
}

func (s *subscriptionMGR) subscriptionsForStream(id string) []*subscription {
	subIDs := make([]*subscription, 0)
	for _, sub := range s.subscriptions {
		if sub.info.Stream == id {
			subIDs = append(subIDs, sub)
		}
	}
	return subIDs
}

// SuspendStream suspends a streamm from firing
func (s *subscriptionMGR) SuspendStream(ctx context.Context, id string) error {
	stream, err := s.streamByID(id)
	if err != nil {
		return err
	}
	stream.suspend()
	// Persist the state change
	_, err = s.storeStream(stream.spec)
	return err
}

// ResumeStream restarts a suspended stream
func (s *subscriptionMGR) ResumeStream(ctx context.Context, id string) error {
	stream, err := s.streamByID(id)
	if err != nil {
		return err
	}
	if err = stream.resume(); err != nil {
		return err
	}
	// Persist the state change
	_, err = s.storeStream(stream.spec)
	return err
}

// subscriptionByID used internally to lookup full objects
func (s *subscriptionMGR) subscriptionByID(id string) (*subscription, error) {
	sub, exists := s.subscriptions[id]
	if !exists {
		return nil, klderrors.Errorf(klderrors.EventStreamsSubscriptionNotFound, id)
	}
	return sub, nil
}

// streamByID used internally to lookup full objects
func (s *subscriptionMGR) streamByID(id string) (*eventStream, error) {
	stream, exists := s.streams[id]
	if !exists {
		return nil, klderrors.Errorf(klderrors.EventStreamsStreamNotFound, id)
	}
	return stream, nil
}

func (s *subscriptionMGR) loadCheckpoint(streamID string) (map[string]*big.Int, error) {
	cpID := checkpointIDPrefix + streamID
	b, err := s.db.Get(cpID)
	if err == leveldb.ErrNotFound {
		return make(map[string]*big.Int), nil
	} else if err != nil {
		return nil, err
	}
	log.Debugf("Loaded checkpoint %s: %s", cpID, string(b))
	var checkpoint map[string]*big.Int
	err = json.Unmarshal(b, &checkpoint)
	if err != nil {
		return nil, err
	}
	return checkpoint, nil
}

func (s *subscriptionMGR) storeCheckpoint(streamID string, checkpoint map[string]*big.Int) error {
	cpID := checkpointIDPrefix + streamID
	b, _ := json.MarshalIndent(&checkpoint, "", "  ")
	log.Debugf("Storing checkpoint %s: %s", cpID, string(b))
	return s.db.Put(cpID, b)
}

func (s *subscriptionMGR) deleteCheckpoint(streamID string) {
	cpID := checkpointIDPrefix + streamID
	s.db.Delete(cpID)
}

func (s *subscriptionMGR) Init() (err error) {
	if s.db, err = kldkvstore.NewLDBKeyValueStore(s.conf.EventLevelDBPath); err != nil {
		return klderrors.Errorf(klderrors.EventStreamsDBLoad, s.conf.EventLevelDBPath, err)
	}
	s.recoverStreams()
	s.recoverSubscriptions()
	return nil
}

func (s *subscriptionMGR) recoverStreams() {
	// Recover all the streams
	iStream := s.db.NewIterator()
	defer iStream.Release()
	for iStream.Next() {
		k := iStream.Key()
		if strings.HasPrefix(k, streamIDPrefix) {
			var streamInfo StreamInfo
			err := json.Unmarshal(iStream.Value(), &streamInfo)
			if err != nil {
				log.Errorf("Failed to recover stream '%s': %s", string(iStream.Value()), err)
				continue
			}
			stream, err := newEventStream(s, &streamInfo)
			if err != nil {
				log.Errorf("Failed to recover stream '%s': %s", streamInfo.ID, err)
			} else {
				s.streams[streamInfo.ID] = stream
			}
		}
	}
}

func (s *subscriptionMGR) recoverSubscriptions() {
	// Recover all the subscriptions
	iSub := s.db.NewIterator()
	defer iSub.Release()
	for iSub.Next() {
		k := iSub.Key()
		if strings.HasPrefix(k, subIDPrefix) {
			var subInfo SubscriptionInfo
			err := json.Unmarshal(iSub.Value(), &subInfo)
			if err != nil {
				log.Errorf("Failed to recover subscription '%s': %s", string(iSub.Value()), err)
				continue
			}
			sub, err := restoreSubscription(s, s.rpc, &subInfo)
			if err != nil {
				log.Errorf("Failed to recover subscription '%s': %s", subInfo.ID, err)
			} else {
				s.subscriptions[subInfo.ID] = sub
			}
		}
	}
}

func (s *subscriptionMGR) Close() {
	log.Infof("Event stream subscription manager shutting down")
	for _, stream := range s.streams {
		stream.stop()
	}
	if !s.closed && s.db != nil {
		s.db.Close()
	}
	s.closed = true
}
