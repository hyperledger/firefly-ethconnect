// Copyright 2018, 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldeth

import (
	"context"
	"net/url"
	"os"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// This module provides an abstraction layer for the RPC structs of the go-ethereum/rpc
// package, as mockable interfaces. There's some complexity and type mapping needed
// to allow this module to be the only one that needs to use the real types

// RPCConf is the standard snippet to include in YAML config for RPC
type RPCConf struct {
	RPC RPCConnOpts `json:"rpc"`
}

// RPCConnOpts configuration params
type RPCConnOpts struct {
	URL string `json:"url"`
}

// RPCConnect wraps rpc.Dial with useful logging, avoiding logging username/password
func RPCConnect(conf *RPCConnOpts) (RPCClientAll, error) {
	u, _ := url.Parse(conf.URL)
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "xxxxxx")
	}
	rpcClient, err := rpc.Dial(conf.URL)
	if err != nil {
		return nil, klderrors.Errorf(klderrors.RPCConnectFailed, u, err)
	}
	log.Infof("New JSON/RPC connection established")
	log.Debugf("JSON/RPC connected to %s", u)
	return &rpcWrapper{rpc: rpcClient}, nil
}

// CobraInitRPC sets the standard command-line parameters for RPC
func CobraInitRPC(cmd *cobra.Command, rconf *RPCConf) {
	cmd.Flags().StringVarP(&rconf.RPC.URL, "rpc-url", "r", os.Getenv("ETH_RPC_URL"), "JSON/RPC URL for Ethereum node")
	return
}

// rpc.RPCClient methods with original types that we expose - only used within this package.
// Other packages use RPCClientAll
type rcpClient interface {
	CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error
	Subscribe(ctx context.Context, namespace string, channel interface{}, args ...interface{}) (*rpc.ClientSubscription, error)
	Close()
}

type rpcWrapper struct {
	rpc rcpClient
}

// RPCClientSubscription local alias type for ClientSubscription
type RPCClientSubscription interface {
	Unsubscribe()
	Err() <-chan error
}

type subWrapper struct {
	s RPCClientSubscription
}

func (sw *subWrapper) Err() <-chan error {
	return sw.s.Err()
}

func (sw *subWrapper) Unsubscribe() {
	sw.s.Unsubscribe()
}

func (w *rpcWrapper) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	if err := kldauth.AuthRPC(ctx, method, args...); err != nil {
		log.Errorf("JSON/RPC %s - not authorized: %s", method, err)
		return klderrors.Errorf(klderrors.Unauthorized)
	}
	log.Tracef("RPC [%s] --> %+v", method, args)
	err := w.rpc.CallContext(ctx, result, method, args...)
	log.Tracef("RPC [%s] <-- %+v", method, result)
	return err
}

func (w *rpcWrapper) Subscribe(ctx context.Context, namespace string, channel interface{}, args ...interface{}) (RPCClientSubscription, error) {
	if err := kldauth.AuthRPCSubscribe(ctx, namespace, channel, args...); err != nil {
		log.Errorf("JSON/RPC Subscribe - not authorized: %s", err)
		return nil, klderrors.Errorf(klderrors.Unauthorized)
	}
	tSub, err := w.rpc.Subscribe(ctx, namespace, channel, args...)
	return &subWrapper{s: tSub}, err
}

func (w *rpcWrapper) Close() {
	w.rpc.Close()
}

// RPCClientAll has both sync and async interfaces (splitting out helps callers with limiting their mocks)
type RPCClientAll interface {
	RPCClosable
	RPCClient
	RPCClientAsync
}

// RPCClosable contains the close
type RPCClosable interface {
	Close()
}

// RPCClient refers to the functions from the ethereum RPC client that we use
type RPCClient interface {
	CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error
}

// RPCClientAsync refers to the async functions from the ethereum RPC client that we use
type RPCClientAsync interface {
	Subscribe(ctx context.Context, namespace string, channel interface{}, args ...interface{}) (RPCClientSubscription, error)
}
