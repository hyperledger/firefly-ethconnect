// Copyright 2018, 2021 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tx

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	"github.com/hyperledger/firefly-ethconnect/internal/utils"
	log "github.com/sirupsen/logrus"
)

const (
	defaultRPCEndpointProp       = "endpoint"
	defaultHealthcheckFrequency  = 30 * time.Second
	defaultAddressbookRetryDelay = 3 * time.Second
	defaultAddressbookMaxRetries = 100
)

// AddressBook looks up RPC URLs based on a remote registry, and optionally
// resolves hostnames to IP addresses using a hosts file
type AddressBook interface {
	lookup(ctx context.Context, addr string) (eth.RPCClient, error)
}

// AddressBookConf configuration
type AddressBookConf struct {
	utils.HTTPRequesterConf
	AddressbookURLPrefix    string                   `json:"urlPrefix"`
	HostsFile               string                   `json:"hostsFile"`
	PropNames               AddressBookPropNamesConf `json:"propNames"`
	RetryDelaySec           *int                     `json:"retryDelaySec,omitempty"`
	HealthcheckFrequencySec *int                     `json:"healthcheckFrequencySec,omitempty"`
	MaxRetries              *int                     `json:"maxRetries,omitempty"`
}

// AddressBookPropNamesConf configures the JSON property names to extract from the GET response on the API
type AddressBookPropNamesConf struct {
	RPCEndpoint string `json:"endpoint"`
}

// NewAddressBook constructor
func NewAddressBook(conf *AddressBookConf, rpcConf *eth.RPCConf) AddressBook {
	ab := &addressBook{
		conf:                conf,
		fallbackRPCEndpoint: rpcConf.RPC.URL,
		hr:                  utils.NewHTTPRequester("Addressbook", &conf.HTTPRequesterConf),
		addrToHost:          make(map[string]string),
		hostToRPC:           make(map[string]*cachedRPC),
	}
	propNames := &conf.PropNames
	if propNames.RPCEndpoint == "" {
		propNames.RPCEndpoint = defaultRPCEndpointProp
	}
	if ab.conf.AddressbookURLPrefix != "" && !strings.HasSuffix(ab.conf.AddressbookURLPrefix, "/") {
		ab.conf.AddressbookURLPrefix += "/"
	}
	ab.healthcheckFrequency = defaultHealthcheckFrequency
	if ab.conf.HealthcheckFrequencySec != nil {
		ab.healthcheckFrequency = time.Duration(*ab.conf.HealthcheckFrequencySec) * time.Second
	}
	ab.retryDelay = defaultAddressbookRetryDelay
	if ab.conf.RetryDelaySec != nil {
		ab.retryDelay = time.Duration(*ab.conf.RetryDelaySec) * time.Second
	}
	ab.maxRetries = defaultAddressbookMaxRetries
	if ab.conf.MaxRetries != nil {
		ab.maxRetries = *ab.conf.MaxRetries
	}
	return ab
}

type cachedRPC struct {
	lastChecked time.Time
	rpc         eth.RPCClientAll
}

type addressBook struct {
	conf                 *AddressBookConf
	hr                   *utils.HTTPRequester
	mtx                  sync.Mutex
	fallbackRPCEndpoint  string
	healthcheckFrequency time.Duration
	addrToHost           map[string]string
	hostToRPC            map[string]*cachedRPC
	retryDelay           time.Duration
	maxRetries           int
}

// testRPC uses a simple net_version JSON/RPC call to test the health of a cached connection
func (ab *addressBook) testRPC(ctx context.Context, rpc eth.RPCClient) bool {
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
		return nil, errors.Errorf(errors.AddressBookLookupBadURL)
	}

	if ab.conf.HostsFile != "" {
		hostsMap, err := utils.ParseHosts(ab.conf.HostsFile)
		if err != nil {
			log.Errorf("Failed to parse hosts file: %s", err)
			return nil, errors.Errorf(errors.AddressBookLookupBadHostsFile)
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
func (ab *addressBook) mapEndpoint(ctx context.Context, endpoint string) (eth.RPCClient, error) {

	// Simple locking on our cache for now (covers long-lived async test+connect operations)
	ab.mtx.Lock()
	defer ab.mtx.Unlock()

	// Hopefully we already have a client in our map, and it's healthy
	cached, ok := ab.hostToRPC[endpoint]
	if ok {
		log.Debugf("Using cached RPC connection for signing")
		if time.Since(cached.lastChecked) < ab.healthcheckFrequency {
			return cached.rpc, nil
		}
		if ab.testRPC(ctx, cached.rpc) {
			cached.lastChecked = time.Now()
			return cached.rpc, nil
		}
		// Clean up our cache entry as it is stale
		log.Warnf("Cached RPC connection failed healthcheck - reconnecting")
		cached.rpc.Close()
		delete(ab.hostToRPC, endpoint)
	}

	url, err := ab.resolveHost(endpoint)
	if err != nil {
		return nil, err
	}

	// Connect and cache the RPC connection
	rpc, err := eth.RPCConnect(&eth.RPCConnOpts{
		URL: url.String(),
	})
	if err != nil {
		return nil, err
	}
	ab.hostToRPC[endpoint] = &cachedRPC{
		lastChecked: time.Now(),
		rpc:         rpc,
	}
	return rpc, err
}

func (ab *addressBook) requestWithRetry(url string) map[string]interface{} {
	retryCount := 0
	for {
		body, err := ab.hr.DoRequest("GET", url, nil)
		if err == nil {
			return body
		}
		if retryCount > ab.maxRetries {
			log.Errorf("Non-404 error from address book. Retries exhausted, falling back to default rpc endpoint: %s", err)
			return nil
		}
		retryCount++
		log.Errorf("Non-404 error from address book - retrying: %s", err)
		time.Sleep(ab.retryDelay)
	}
}

// lookup the RPC URL to use for a given from address, performing hostname resolution
// based on a custom hosts file (if configured)
func (ab *addressBook) lookup(ctx context.Context, fromAddr string) (rpc eth.RPCClient, err error) {

	// First check if we already know the base (non host translated) endpoint  to use for this address
	// Simple locking on our cache for now (covers long-lived async test+connect operations)
	ab.mtx.Lock()
	endpoint, found := ab.addrToHost[fromAddr]
	ab.mtx.Unlock()

	if !found || endpoint == "" {
		log.Infof("Resolving signing address (not cached): %s", fromAddr)
		url := ab.conf.AddressbookURLPrefix + fromAddr
		body := ab.requestWithRetry(url)
		if body == nil {
			if ab.fallbackRPCEndpoint == "" {
				return nil, errors.Errorf(errors.AddressBookLookupNotFound)
			}
			endpoint = ab.fallbackRPCEndpoint
		} else {
			endpoint, err = ab.hr.GetResponseString(body, ab.conf.PropNames.RPCEndpoint, false)
			if err != nil {
				return nil, err
			}
			// We've found a conclusive hit. Use this endpoint from now on for this address.
			ab.mtx.Lock()
			ab.addrToHost[fromAddr] = endpoint
			ab.mtx.Unlock()
		}
	}

	return ab.mapEndpoint(ctx, endpoint)
}
