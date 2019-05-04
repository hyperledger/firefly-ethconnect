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

// SubscriptionManagerConf configuration
type SubscriptionManagerConf struct {
	LevelDBPath string `json:"db"`
}

type subscriptionManager struct {
	conf    *SubscriptionManagerConf
	rpcConf *kldeth.RPCConnOpts
	db      kvStore
	rpc     kldeth.RPCClientAll
}

// CobraInitSubscriptionManager standard naming for cobra command params
func CobraInitSubscriptionManager(cmd *cobra.Command, conf *SubscriptionManagerConf) {
	cmd.Flags().StringVarP(&conf.LevelDBPath, "events-db", "E", "", "Level DB location for subscription management")
}

func (s *subscriptionManager) AddRoutes(router *httprouter.Router) {
}

// NewSubscriptionManager construtor
func NewSubscriptionManager(conf *SubscriptionManagerConf, rpcConf *kldeth.RPCConnOpts) SubscriptionManager {
	sm := &subscriptionManager{
		conf:    conf,
		rpcConf: rpcConf,
	}
	return sm
}

func (s *subscriptionManager) Init() (err error) {
	if s.db, err = newLDBKeyValueStore(s.conf.LevelDBPath); err != nil {
		return fmt.Errorf("Failed to open DB at %s: %s", s.conf.LevelDBPath, err)
	}
	if s.rpc, err = kldeth.RPCConnect(s.rpcConf); err != nil {
		return err
	}
	return nil
}

func (s *subscriptionManager) Close() {
	if s.db != nil {
		s.db.Close()
	}
	if s.rpc != nil {
		s.rpc.Close()
	}
}
