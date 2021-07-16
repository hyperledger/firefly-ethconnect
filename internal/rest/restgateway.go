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

package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kaleido-io/ethconnect/internal/auth"
	"github.com/kaleido-io/ethconnect/internal/contracts"
	"github.com/kaleido-io/ethconnect/internal/errors"
	"github.com/kaleido-io/ethconnect/internal/eth"
	"github.com/kaleido-io/ethconnect/internal/kafka"
	"github.com/kaleido-io/ethconnect/internal/messages"
	"github.com/kaleido-io/ethconnect/internal/tx"
	"github.com/kaleido-io/ethconnect/internal/utils"
	"github.com/kaleido-io/ethconnect/internal/ws"

	"github.com/Shopify/sarama"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	// MaxHeaderSize max size of content
	MaxHeaderSize = 16 * 1024
)

// ReceiptStoreConf is the common configuration for all receipt stores
type ReceiptStoreConf struct {
	MaxDocs             int `json:"maxDocs"`
	QueryLimit          int `json:"queryLimit"`
	RetryInitialDelayMS int `json:"retryInitialDelay"`
	RetryTimeoutMS      int `json:"retryTimeout"`
}

// MongoDBReceiptStoreConf is the configuration for a MongoDB receipt store
type MongoDBReceiptStoreConf struct {
	ReceiptStoreConf
	URL              string `json:"url"`
	Database         string `json:"database"`
	Collection       string `json:"collection"`
	ConnectTimeoutMS int    `json:"connectTimeout"`
}

// LevelDBReceiptStoreConf is the configuration for a LevelDB receipt store
type LevelDBReceiptStoreConf struct {
	ReceiptStoreConf
	Path string `json:"path"`
}

// RESTGatewayConf defines the YAML config structure for a webhooks bridge instance
type RESTGatewayConf struct {
	Kafka    kafka.KafkaCommonConf              `json:"kafka"`
	MongoDB  MongoDBReceiptStoreConf            `json:"mongodb"`
	LevelDB  LevelDBReceiptStoreConf            `json:"leveldb"`
	MemStore ReceiptStoreConf                   `json:"memstore"`
	OpenAPI  contracts.SmartContractGatewayConf `json:"openapi"`
	HTTP     struct {
		LocalAddr string          `json:"localAddr"`
		Port      int             `json:"port"`
		TLS       utils.TLSConfig `json:"tls"`
	} `json:"http"`
	WebhooksDirectConf
}

// RESTGateway as the HTTP gateway interface for ethconnect
type RESTGateway struct {
	printYAML       *bool
	conf            RESTGatewayConf
	kafka           kafka.KafkaCommon
	srv             *http.Server
	sendCond        *sync.Cond
	pendingMsgs     map[string]bool
	successMsgs     map[string]*sarama.ProducerMessage
	failedMsgs      map[string]error
	receipts        *receiptStore
	webhooks        *webhooks
	smartContractGW contracts.SmartContractGateway
	ws              ws.WebSocketServer
}

// Conf gets the config for this bridge
func (g *RESTGateway) Conf() *RESTGatewayConf {
	return &g.conf
}

// SetConf sets the config for this bridge
func (g *RESTGateway) SetConf(conf *RESTGatewayConf) {
	g.conf = *conf
}

// ValidateConf validates the config
func (g *RESTGateway) ValidateConf() (err error) {
	if !utils.AllOrNoneReqd(g.conf.MongoDB.URL, g.conf.MongoDB.Database, g.conf.MongoDB.Collection) {
		err = errors.Errorf(errors.ConfigRESTGatewayRequiredReceiptStore)
		return
	}
	if g.conf.MongoDB.QueryLimit < 1 {
		g.conf.MongoDB.QueryLimit = 100
	}
	if g.conf.LevelDB.QueryLimit < 1 {
		g.conf.LevelDB.QueryLimit = 100
	}
	if g.conf.OpenAPI.StoragePath != "" && g.conf.RPC.URL == "" {
		err = errors.Errorf(errors.ConfigRESTGatewayRequiredRPC)
		return
	}
	return
}

// NewRESTGateway constructor
func NewRESTGateway(printYAML *bool) (g *RESTGateway) {
	g = &RESTGateway{
		printYAML:   printYAML,
		sendCond:    sync.NewCond(&sync.Mutex{}),
		pendingMsgs: make(map[string]bool),
		successMsgs: make(map[string]*sarama.ProducerMessage),
		failedMsgs:  make(map[string]error),
		ws:          ws.NewWebSocketServer(),
	}
	return
}

// CobraInit retruns a cobra command to configure this KafkaBridge
func (g *RESTGateway) CobraInit(cmdName string) (cmd *cobra.Command) {
	cmd = &cobra.Command{
		Use:   cmdName,
		Short: "REST Gateway",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			err = g.Start()
			return
		},
		PreRunE: func(cmd *cobra.Command, args []string) (err error) {
			if len(g.conf.Kafka.Brokers) > 0 {
				if err = kafka.KafkaValidateConf(&g.conf.Kafka); err != nil {
					return
				}
			} else {
				if err = validateWebhooksDirectConf(&g.conf.WebhooksDirectConf); err != nil {
					return
				}
			}

			// The simple commandline interface requires the TLS configuration for
			// both Kafka and HTTP is the same. Only the YAML configuration allows
			// them to be different
			g.conf.HTTP.TLS = g.conf.Kafka.TLS

			err = g.ValidateConf()
			return
		},
	}
	kafka.KafkaCommonCobraInit(cmd, &g.conf.Kafka)
	eth.CobraInitRPC(cmd, &g.conf.RPCConf)
	tx.CobraInitTxnProcessor(cmd, &g.conf.TxnProcessorConf)
	contracts.CobraInitContractGateway(cmd, &g.conf.OpenAPI)
	cmd.Flags().IntVarP(&g.conf.MaxInFlight, "maxinflight", "m", utils.DefInt("WEBHOOKS_MAX_INFLIGHT", 0), "Maximum messages to hold in-flight")
	cmd.Flags().StringVarP(&g.conf.HTTP.LocalAddr, "listen-addr", "L", os.Getenv("WEBHOOKS_LISTEN_ADDR"), "Local address to listen on")
	cmd.Flags().IntVarP(&g.conf.HTTP.Port, "listen-port", "l", utils.DefInt("WEBHOOKS_LISTEN_PORT", 8080), "Port to listen on")
	cmd.Flags().StringVarP(&g.conf.MongoDB.URL, "mongodb-url", "M", os.Getenv("MONGODB_URL"), "MongoDB URL for a receipt store")
	cmd.Flags().StringVarP(&g.conf.MongoDB.Database, "mongodb-database", "D", os.Getenv("MONGODB_DATABASE"), "MongoDB receipt store database")
	cmd.Flags().StringVarP(&g.conf.MongoDB.Collection, "mongodb-receipt-collection", "R", os.Getenv("MONGODB_COLLECTION"), "MongoDB receipt store collection")
	cmd.Flags().IntVarP(&g.conf.MongoDB.MaxDocs, "mongodb-receipt-maxdocs", "X", utils.DefInt("MONGODB_MAXDOCS", 0), "Receipt store capped size (new collections only)")
	cmd.Flags().IntVarP(&g.conf.MongoDB.QueryLimit, "mongodb-query-limit", "Q", utils.DefInt("MONGODB_QUERYLIM", 0), "Maximum docs to return on a rest call (cap on limit)")
	cmd.Flags().IntVarP(&g.conf.MemStore.MaxDocs, "memstore-receipt-maxdocs", "v", utils.DefInt("MEMSTORE_MAXDOCS", 10), "In-memory receipt store capped size")
	cmd.Flags().IntVarP(&g.conf.MemStore.QueryLimit, "memstore-query-limit", "V", utils.DefInt("MEMSTORE_QUERYLIM", 0), "In-memory maximum docs to return on a rest call")
	cmd.Flags().IntVarP(&g.conf.LevelDB.QueryLimit, "leveldb-query-limit", "B", utils.DefInt("LEVELDB_QUERYLIM", 0), "Maximum docs to return on a rest call (cap on limit)")
	return
}

type statusMsg struct {
	OK bool `json:"ok"`
}

type errMsg struct {
	Message string `json:"error"`
}

func (g *RESTGateway) statusHandler(res http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	reply, _ := json.Marshal(&statusMsg{OK: true})
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(200)
	res.Write(reply)
	return
}

func (g *RESTGateway) sendError(res http.ResponseWriter, msg string, code int) {
	reply, _ := json.Marshal(&errMsg{Message: msg})
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(code)
	res.Write(reply)
	return
}

// DispatchMsgAsync is the rest2eth interface method for async dispatching of messages (via our webhook logic)
func (g *RESTGateway) DispatchMsgAsync(ctx context.Context, msg map[string]interface{}, ack bool) (*messages.AsyncSentMsg, error) {
	reply, _, err := g.webhooks.processMsg(ctx, msg, ack)
	return reply, err
}

func (g *RESTGateway) newAccessTokenContextHandler(parent http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {

		// Extract an access token from bearer token (only - no support for query params)
		accessToken := ""
		hSplit := strings.SplitN(req.Header.Get("Authorization"), " ", 2)
		if len(hSplit) == 2 && strings.ToLower(hSplit[0]) == "bearer" {
			accessToken = hSplit[1]
		}
		authCtx, err := auth.WithAuthContext(req.Context(), accessToken)
		if err != nil {
			log.Errorf("Error getting auth context: %s", err)
			g.sendError(res, "Unauthorized", 401)
			return
		}

		parent.ServeHTTP(res, req.WithContext(authCtx))
	})
}

// Start kicks off the HTTP listener and router
func (g *RESTGateway) Start() (err error) {

	if *g.printYAML {
		b, err := utils.MarshalToYAML(&g.conf)
		print("# YAML Configuration snippet for REST Gateway\n" + string(b))
		return err
	}

	tlsConfig, err := utils.CreateTLSConfiguration(&g.conf.HTTP.TLS)
	if err != nil {
		return
	}

	router := httprouter.New()

	var processor tx.TxnProcessor
	var rpcClient eth.RPCClient
	if g.conf.RPC.URL != "" || g.conf.OpenAPI.StoragePath != "" {
		rpcClient, err = eth.RPCConnect(&g.conf.RPC)
		if err != nil {
			return err
		}
		processor = tx.NewTxnProcessor(&g.conf.TxnProcessorConf, &g.conf.RPCConf)
		processor.Init(rpcClient)
	}

	g.ws.AddRoutes(router)

	if g.conf.OpenAPI.StoragePath != "" {
		g.smartContractGW, err = contracts.NewSmartContractGateway(&g.conf.OpenAPI, &g.conf.TxnProcessorConf, rpcClient, processor, g, g.ws)
		if err != nil {
			return err
		}
		g.smartContractGW.AddRoutes(router)
	}

	var receiptStoreConf *ReceiptStoreConf
	var receiptStorePersistence ReceiptStorePersistence
	if g.conf.MongoDB.URL != "" {
		receiptStoreConf = &g.conf.MongoDB.ReceiptStoreConf
		mongoStore := newMongoReceipts(&g.conf.MongoDB)
		receiptStorePersistence = mongoStore
		if err = mongoStore.connect(); err != nil {
			return
		}
	} else if g.conf.LevelDB.Path != "" {
		receiptStoreConf = &g.conf.LevelDB.ReceiptStoreConf
		leveldbStore, errResult := newLevelDBReceipts(&g.conf.LevelDB)
		if errResult != nil {
			err = errResult
			return
		}
		receiptStorePersistence = leveldbStore
	} else {
		receiptStoreConf = &g.conf.MemStore
		memStore := newMemoryReceipts(&g.conf.MemStore)
		receiptStorePersistence = memStore
	}

	router.GET("/status", g.statusHandler)
	g.receipts = newReceiptStore(receiptStoreConf, receiptStorePersistence, g.smartContractGW)
	g.receipts.addRoutes(router)
	if len(g.conf.Kafka.Brokers) > 0 {
		wk := newWebhooksKafka(&g.conf.Kafka, g.receipts)
		g.webhooks = newWebhooks(wk, g.smartContractGW)
	} else {
		wd := newWebhooksDirect(&g.conf.WebhooksDirectConf, processor, g.receipts)
		g.webhooks = newWebhooks(wd, g.smartContractGW)
	}
	g.webhooks.addRoutes(router)

	g.srv = &http.Server{
		Addr:           fmt.Sprintf("%s:%d", g.conf.HTTP.LocalAddr, g.conf.HTTP.Port),
		TLSConfig:      tlsConfig,
		Handler:        g.newAccessTokenContextHandler(router),
		MaxHeaderBytes: MaxHeaderSize,
	}

	readyToListen := make(chan bool)
	gwDone := make(chan error)
	svrDone := make(chan error)

	go func() {
		<-readyToListen
		log.Printf("HTTP server listening on %s", g.srv.Addr)
		err := g.srv.ListenAndServe()
		if err != nil {
			log.Errorf("Listening ended with: %s", err)
		}
		svrDone <- err
	}()
	go func() {
		err := g.webhooks.run()
		if err != nil {
			log.Errorf("Webhooks Kafka bridge ended with: %s", err)
		}
		gwDone <- err
	}()
	for !g.webhooks.isInitialized() {
		time.Sleep(250 * time.Millisecond)
	}
	readyToListen <- true

	// Clean up on SIGINT
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	// Complete the main routine if any child ends, or SIGINT
	select {
	case err = <-gwDone:
		break
	case err = <-svrDone:
		break
	case <-signals:
		break
	}

	// Ensure we shutdown the server
	if g.smartContractGW != nil {
		g.smartContractGW.Shutdown()
	}
	log.Infof("Shutting down HTTP server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	g.srv.Shutdown(ctx)
	defer cancel()

	return
}
