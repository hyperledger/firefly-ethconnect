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
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/kaleido-io/ethconnect/internal/klderrors"

	log "github.com/sirupsen/logrus"
)

type webhookAction struct {
	es   *eventStream
	spec *webhookActionInfo
}

func newWebhookAction(es *eventStream, spec *webhookActionInfo) (*webhookAction, error) {
	if spec == nil || spec.URL == "" {
		return nil, klderrors.Errorf(klderrors.EventStreamsWebhookNoURL)
	}
	if _, err := url.Parse(spec.URL); err != nil {
		return nil, klderrors.Errorf(klderrors.EventStreamsWebhookInvalidURL)
	}
	if spec.RequestTimeoutSec == 0 {
		spec.RequestTimeoutSec = 30000
	}
	return &webhookAction{
		es:   es,
		spec: spec,
	}, nil
}

// attemptWebhookAction performs a single attempt of a webhook action
func (w *webhookAction) attemptBatch(batchNumber, attempt uint64, events []*eventData) error {
	// We perform DNS resolution before each attempt, to exclude private IP address ranges from the target
	esID := w.es.spec.ID
	u, _ := url.Parse(w.spec.URL)
	addr, err := net.ResolveIPAddr("ip4", u.Hostname())
	if err != nil {
		return err
	}
	if w.es.isAddressUnsafe(addr) {
		err := klderrors.Errorf(klderrors.EventStreamsWebhookProhibitedAddress, u.Hostname())
		log.Errorf(err.Error())
		return err
	}
	// Set the timeout
	var transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: w.spec.TLSkipHostVerify,
	}
	netClient := &http.Client{
		Timeout:   time.Duration(w.spec.RequestTimeoutSec) * time.Second,
		Transport: transport,
	}
	log.Infof("%s: POST --> %s [%s] (attempt=%d)", esID, u.String(), addr.String(), attempt)
	reqBytes, err := json.Marshal(&events)
	var req *http.Request
	if err == nil {
		req, err = http.NewRequest("POST", u.String(), bytes.NewReader(reqBytes))
	}
	if err == nil {
		var res *http.Response
		req.Header.Set("Content-Type", "application/json")
		for h, v := range w.spec.Headers {
			req.Header.Set(h, v)
		}
		res, err = netClient.Do(req)
		if err == nil {
			ok := (res.StatusCode >= 200 && res.StatusCode < 300)
			log.Infof("%s: POST <-- %s [%d] ok=%t", esID, u.String(), res.StatusCode, ok)
			if !ok || log.IsLevelEnabled(log.DebugLevel) {
				bodyBytes, _ := ioutil.ReadAll(res.Body)
				log.Infof("%s: Response body: %s", esID, string(bodyBytes))
			}
			if !ok {
				err = klderrors.Errorf(klderrors.EventStreamsWebhookFailedHTTPStatus, esID, res.StatusCode)
			}
		}
	}
	if err != nil {
		log.Errorf("%s: POST %s failed (attempt=%d): %s", esID, u.String(), attempt, err)
	}
	return err
}
