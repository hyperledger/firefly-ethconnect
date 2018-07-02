 # Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=ethconnect
BINARY_UNIX=$(BINARY_NAME)-tux
BINARY_MAC=$(BINARY_NAME)-mac
BINARY_WIN=$(BINARY_NAME)-win

all: deps build test
build: 
		$(GOBUILD) -o $(BINARY_NAME) -v
test:
		$(GOTEST)  ./... -cover -coverprofile=coverage.txt -covermode=atomic
clean: 
		$(GOCLEAN)
		rm -f $(BINARY_NAME)
		rm -f $(BINARY_UNIX)
run:
		$(GOBUILD) -o $(BINARY_NAME) -v ./...
		./$(BINARY_NAME)
deps:
		$(GOGET) github.com/ethereum/go-ethereum
		$(GOGET) github.com/sirupsen/logrus
		$(GOGET) github.com/spf13/cobra
		$(GOGET) github.com/bsm/sarama-cluster
		$(GOGET) github.com/Shopify/sarama
		$(GOGET) github.com/nu7hatch/gouuid
		$(GOGET) github.com/stretchr/testify/assert
		$(GOGET) github.com/golang/mock/gomock

build-linux:
		GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BINARY_UNIX) -v
build-mac:
		GOOS=darwin GOARCH=amd64 $(GOBUILD) -o $(BINARY_MAC) -v
build-win:
		GOOS=windows GOARCH=amd64 $(GOBUILD) -o $(BINARY_WIN) -v
