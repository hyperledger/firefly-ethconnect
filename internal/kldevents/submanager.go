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
	"fmt"

	"github.com/julienschmidt/httprouter"
	"github.com/spf13/cobra"

	"github.com/kaleido-io/ethconnect/internal/kldeth"
)

// SubscriptionManager provides REST APIs for managing events
type SubscriptionManager interface {
	Init() error
	AddRoutes(router *httprouter.Router)
	Close()
}

type subscriptionManager interface {
	actionByID(string) (*action, error)
	subscriptionByID(string) (*subscription, error)
}

// SubscriptionManagerConf configuration
type SubscriptionManagerConf struct {
	LevelDBPath string `json:"db"`
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
}

func (s *subscriptionMGR) AddRoutes(router *httprouter.Router) {
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

func (s *subscriptionMGR) subscriptionByID(id string) (*subscription, error) {
	sub, exists := s.subscriptions[id]
	if !exists {
		return nil, fmt.Errorf("Subscription with ID '%s' not found", id)
	}
	return sub, nil
}

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
