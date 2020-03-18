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

package kldtx

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

const (
	defaultRPCEndpointProp = "endpoint"
)

// AddressBook looks up RPC URLs based on a remote registry, and optionally
// resolves hostnames to IP addresses using a hosts file
type AddressBook interface {
	lookup(ctx context.Context, addr string) (kldeth.RPCClient, error)
}

// AddressBookConf configuration
type AddressBookConf struct {
	kldutils.HTTPRequesterConf
	AddressbookURLPrefix string                   `json:"urlPrefix"`
	HostsFile            string                   `json:"hostsFile"`
	PropNames            AddressBookPropNamesConf `json:"propNames"`
}

// AddressBookPropNamesConf configures the JSON property names to extract from the GET response on the API
type AddressBookPropNamesConf struct {
	RPCEndpoint string `json:"endpoint"`
}

// NewAddressBook construtor
func NewAddressBook(conf *AddressBookConf, rpcConf *kldeth.RPCConf) AddressBook {
	ab := &addressBook{
		conf:                conf,
		fallbackRPCEndpoint: rpcConf.RPC.URL,
		hr:                  kldutils.NewHTTPRequester("Addressbook", &conf.HTTPRequesterConf),
		addrToHost:          make(map[string]string),
		hostToRPC:           make(map[string]kldeth.RPCClientAll),
	}
	propNames := &conf.PropNames
	if propNames.RPCEndpoint == "" {
		propNames.RPCEndpoint = defaultRPCEndpointProp
	}
	if ab.conf.AddressbookURLPrefix != "" && !strings.HasSuffix(ab.conf.AddressbookURLPrefix, "/") {
		ab.conf.AddressbookURLPrefix += "/"
	}
	return ab
}

type addressBook struct {
	conf                *AddressBookConf
	hr                  *kldutils.HTTPRequester
	mtx                 sync.Mutex
	fallbackRPCEndpoint string
	addrToHost          map[string]string
	hostToRPC           map[string]kldeth.RPCClientAll
}

// testRPC uses a simple net_version JSON/RPC call to test the health of a cached connection
func (ab *addressBook) testRPC(ctx context.Context, rpc kldeth.RPCClient) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var netID string
	if err := rpc.CallContext(ctx, &netID, "net_version"); err != nil {
		log.Errorf("Stale RPC connection in pool: %s", err)
		return false
	}
	return true
}

func (ab *addressBook) resolveHost(endpoint string) (*url.URL, error) {
	// We need to connect, but first we need to resolve the hostname to an IP
	url, err := url.Parse(endpoint)
	if err != nil {
		log.Errorf("Invalid URL '%s': %s", endpoint, err)
		return nil, klderrors.Errorf(klderrors.AddressBookLookupBadURL)
	}

	if ab.conf.HostsFile != "" {
		hostsMap, err := kldutils.ParseHosts(ab.conf.HostsFile)
		if err != nil {
			log.Errorf("Failed to parse hosts file: %s", err)
			return nil, klderrors.Errorf(klderrors.AddressBookLookupBadHostsFile)
		}
		splitHostPort := strings.Split(url.Host, ":")
		if mappedHost, ok := hostsMap[splitHostPort[0]]; ok && mappedHost != "" {
			log.Debugf("Resolved %s to %s", url.Host, mappedHost)
			url.Host = mappedHost
			if len(splitHostPort) > 1 {
				url.Host += ":" + splitHostPort[1]
			}
		}
	}
	return url, nil
}

// mapEndpoint takes an RPC connect endpoint (prior to host resolution) and maps
// it to a cached RPC connection. Or creates a new connection and caches it.
func (ab *addressBook) mapEndpoint(ctx context.Context, endpoint string) (kldeth.RPCClient, error) {

	// Simple locking on our cache for now (covers long-lived async test+connect operations)
	ab.mtx.Lock()
	defer ab.mtx.Unlock()

	// Hopefully we already have a client in our map, and it's healthy
	rpc, ok := ab.hostToRPC[endpoint]
	if ok {
		if ab.testRPC(ctx, rpc) {
			log.Infof("Using cached RPC connection for signing")
			return rpc, nil
		}
		// Clean up our cache entry as it is stale
		rpc.Close()
		delete(ab.hostToRPC, endpoint)
	}

	url, err := ab.resolveHost(endpoint)
	if err != nil {
		return nil, err
	}

	// Connect and cache the RPC connection
	if rpc, err = kldeth.RPCConnect(&kldeth.RPCConnOpts{
		URL: url.String(),
	}); err != nil {
		return nil, err
	}
	ab.hostToRPC[endpoint] = rpc
	return rpc, err
}

// lookup the RPC URL to use for a given from address, performing hostname resolution
// based on a custom hosts file (if configured)
func (ab *addressBook) lookup(ctx context.Context, fromAddr string) (kldeth.RPCClient, error) {
	// First check if we already know the base (non host translated) endpoint
	// to use for this address
	log.Infof("Resolving signing address: %s", fromAddr)
	endpoint, found := ab.addrToHost[fromAddr]
	if !found || endpoint == "" {
		url := ab.conf.AddressbookURLPrefix + fromAddr
		body, err := ab.hr.DoRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		if body == nil {
			if ab.fallbackRPCEndpoint == "" {
				return nil, klderrors.Errorf(klderrors.AddressBookLookupNotFound)
			}
			endpoint = ab.fallbackRPCEndpoint
		} else {
			endpoint, err = ab.hr.GetResponseString(body, ab.conf.PropNames.RPCEndpoint, false)
			if err != nil {
				return nil, err
			}
			// We've found a conclusive hit. Use this endpoint from now on for this address.
			ab.addrToHost[fromAddr] = endpoint
		}
	}

	return ab.mapEndpoint(ctx, endpoint)
}
