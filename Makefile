VGO=go # Set to vgo if building in Go 1.10
BINARY_NAME=ethconnect
BINARY_UNIX=$(BINARY_NAME)-tux
BINARY_MAC=$(BINARY_NAME)-mac
BINARY_WIN=$(BINARY_NAME)-win

.DELETE_ON_ERROR:
GOFILES := $(shell find . -name '*.go' -print)

all: deps build test
build: ethbinding.so
		$(VGO) build -ldflags "-X main.buildDate=`date -u +\"%Y-%m-%dT%H:%M:%SZ\"` -X main.buildVersion=$(BUILD_VERSION)" -tags=prod -o $(BINARY_NAME) -v
ethbinding.so:	  
	  go build -buildmode=plugin github.com/kaleido-io/ethbinding
coverage.txt: $(GOFILES)
		$(VGO) test  ./... -cover -coverprofile=coverage.txt -covermode=atomic -timeout 30s
coverage.html:
	  $(VGO) tool cover -html=coverage.txt
mocks:
	  mockgen github.com/Shopify/sarama Client,ConsumerGroup,ConsumerGroupSession,ConsumerGroupClaim > internal/kldkafka/mock_sarama/sarama_mocks.go
test: coverage.txt
coverage: coverage.txt coverage.html
clean: force
		$(VGO) clean
		rm -f coverage.txt
		rm -f ethbinding.so
		rm -f $(BINARY_NAME)
		rm -f $(BINARY_UNIX)
force: ;
run:
		$(VGO) -o $(BINARY_NAME) -v ./...
		./$(BINARY_NAME)
deps:
		$(VGO) get
build-linux:
		GOOS=linux GOARCH=amd64 $(VGO) build -o $(BINARY_UNIX) -v
build-mac:
		GOOS=darwin GOARCH=amd64 $(VGO) build -o $(BINARY_MAC) -v
build-win:
		GOOS=windows GOARCH=amd64 $(VGO) build -o $(BINARY_WIN) -v

