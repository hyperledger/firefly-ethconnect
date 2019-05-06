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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

const (
	subIDPrefix    = "/subscriptions/"
	actionIDPrefix = "/actions/"
)

// SubscriptionManager provides REST APIs for managing events
type SubscriptionManager interface {
	Init() error
	AddAction(spec *ActionInfo) (*ActionInfo, error)
	Actions() []*ActionInfo
	ActionByID(id string) *ActionInfo
	SuspendAction(id string) error
	ResumeAction(id string) error
	DeleteAction(id string) error
	AddSubscription(addr *kldbind.Address, event *kldbind.ABIEvent, actionID string) (*SubscriptionInfo, error)
	Subscriptions() []*SubscriptionInfo
	SubscriptionByID(id string) *SubscriptionInfo
	DeleteSubscription(id string) error
	Close()
}

type subscriptionManager interface {
	actionByID(string) (*action, error)
	subscriptionByID(string) (*subscription, error)
}

// SubscriptionManagerConf configuration
type SubscriptionManagerConf struct {
	LevelDBPath     string `json:"db"`
	AllowPrivateIPs bool   `json:"allowPrivateIPs,omitempty"`
}

type subscriptionMGR struct {
	conf          *SubscriptionManagerConf
	rpcConf       *kldeth.RPCConnOpts
	db            kvStore
	rpc           kldeth.RPCClientAll
	subscriptions map[string]*subscription
	actions       map[string]*action
}

// CobraInitSubscriptionManager standard naming for cobra command params
func CobraInitSubscriptionManager(cmd *cobra.Command, conf *SubscriptionManagerConf) {
	cmd.Flags().StringVarP(&conf.LevelDBPath, "events-db", "E", "", "Level DB location for subscription management")
	cmd.Flags().BoolVarP(&conf.AllowPrivateIPs, "events-privips", "I", false, "Allow private IPs in Webhooks")
}

// NewSubscriptionManager construtor
func NewSubscriptionManager(conf *SubscriptionManagerConf, rpcConf *kldeth.RPCConnOpts) SubscriptionManager {
	sm := &subscriptionMGR{
		conf:          conf,
		rpcConf:       rpcConf,
		subscriptions: make(map[string]*subscription),
		actions:       make(map[string]*action),
	}
	return sm
}

// SubscriptionByID used externally to get serializable details
func (s *subscriptionMGR) SubscriptionByID(id string) *SubscriptionInfo {
	sub, err := s.subscriptionByID(id)
	if err != nil {
		log.Warnf("Query failed: %s", err)
		return nil
	}
	return sub.info
}

// Subscriptions used externally to get list subscriptions
func (s *subscriptionMGR) Subscriptions() []*SubscriptionInfo {
	l := make([]*SubscriptionInfo, 0, len(s.subscriptions))
	for _, sub := range s.subscriptions {
		l = append(l, sub.info)
	}
	return l
}

// AddSubscription adds a new subscription
func (s *subscriptionMGR) AddSubscription(addr *kldbind.Address, event *kldbind.ABIEvent, actionID string) (*SubscriptionInfo, error) {
	i := &SubscriptionInfo{
		ID:     subIDPrefix + kldutils.UUIDv4(),
		Event:  kldbind.MarshalledABIEvent{E: *event},
		Action: actionID,
	}
	// Create it
	sub, err := newSubscription(s, s.rpc, addr, i)
	if err != nil {
		return nil, err
	}
	s.subscriptions[sub.info.ID] = sub
	return s.storeSubscription(sub.info)
}

// DeleteSubscription deletes an action
func (s *subscriptionMGR) DeleteSubscription(id string) error {
	sub, err := s.subscriptionByID(id)
	if err != nil {
		return err
	}
	delete(s.subscriptions, sub.info.ID)
	sub.unsubscribe()
	if err = s.db.Delete(sub.info.ID); err != nil {
		return err
	}
	return nil
}

func (s *subscriptionMGR) storeSubscription(info *SubscriptionInfo) (*SubscriptionInfo, error) {
	infoBytes, _ := json.MarshalIndent(info, "", "  ")
	if err := s.db.Put(info.ID, infoBytes); err != nil {
		return nil, fmt.Errorf("Failed to store action: %s", err)
	}
	return info, nil
}

// ActionByID used externally to get serializable details
func (s *subscriptionMGR) ActionByID(id string) *ActionInfo {
	action, err := s.actionByID(id)
	if err != nil {
		log.Warnf("Query failed: %s", err)
		return nil
	}
	return action.spec
}

// Actions used externally to get list actions
func (s *subscriptionMGR) Actions() []*ActionInfo {
	l := make([]*ActionInfo, 0, len(s.subscriptions))
	for _, action := range s.actions {
		l = append(l, action.spec)
	}
	return l
}

// AddAction adds a new action
func (s *subscriptionMGR) AddAction(spec *ActionInfo) (*ActionInfo, error) {
	spec.ID = actionIDPrefix + kldutils.UUIDv4()
	action, err := newAction(s.conf.AllowPrivateIPs, spec)
	if err != nil {
		return nil, err
	}
	s.actions[action.spec.ID] = action
	return s.storeAction(action.spec)
}

func (s *subscriptionMGR) storeAction(spec *ActionInfo) (*ActionInfo, error) {
	infoBytes, _ := json.MarshalIndent(spec, "", "  ")
	if err := s.db.Put(spec.ID, infoBytes); err != nil {
		return nil, fmt.Errorf("Failed to store action: %s", err)
	}
	return spec, nil
}

// DeleteAction deletes an action
func (s *subscriptionMGR) DeleteAction(id string) error {
	action, err := s.actionByID(id)
	if err != nil {
		return err
	}
	var subIDs []string
	for _, sub := range s.subscriptions {
		if sub.info.Action == action.spec.ID {
			subIDs = append(subIDs, sub.info.ID)
		}
	}
	if len(subIDs) != 0 {
		return fmt.Errorf("The following subscriptions are still attached: %s", strings.Join(subIDs, ","))
	}
	delete(s.actions, action.spec.ID)
	action.stop()
	if err = s.db.Delete(action.spec.ID); err != nil {
		return err
	}
	return nil
}

// SuspendAction suspends an action from firing
func (s *subscriptionMGR) SuspendAction(id string) error {
	action, err := s.actionByID(id)
	if err != nil {
		return err
	}
	action.suspend()
	// Persist the state change
	_, err = s.storeAction(action.spec)
	return err
}

// ResumeAction restarts a suspended action
func (s *subscriptionMGR) ResumeAction(id string) error {
	action, err := s.actionByID(id)
	if err != nil {
		return err
	}
	if err = action.resume(); err != nil {
		return err
	}
	// Persist the state change
	_, err = s.storeAction(action.spec)
	return err
}

// subscriptionByID used internally to lookup full objects
func (s *subscriptionMGR) subscriptionByID(id string) (*subscription, error) {
	sub, exists := s.subscriptions[id]
	if !exists {
		return nil, fmt.Errorf("Subscription with ID '%s' not found", id)
	}
	return sub, nil
}

// actionByID used internally to lookup full objects
func (s *subscriptionMGR) actionByID(id string) (*action, error) {
	action, exists := s.actions[id]
	if !exists {
		return nil, fmt.Errorf("Action with ID '%s' not found", id)
	}
	return action, nil
}

func (s *subscriptionMGR) Init() (err error) {
	if s.db, err = newLDBKeyValueStore(s.conf.LevelDBPath); err != nil {
		return fmt.Errorf("Failed to open DB at %s: %s", s.conf.LevelDBPath, err)
	}
	if s.rpc, err = kldeth.RPCConnect(s.rpcConf); err != nil {
		return err
	}
	return nil
}

func (s *subscriptionMGR) Close() {
	if s.db != nil {
		s.db.Close()
	}
	if s.rpc != nil {
		s.rpc.Close()
	}
}
