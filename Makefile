VGO=go # Set to vgo if building in Go 1.10
BINARY_NAME=ethconnect
BINARY_UNIX=$(BINARY_NAME)-tux
BINARY_MAC=$(BINARY_NAME)-mac
BINARY_WIN=$(BINARY_NAME)-win

.DELETE_ON_ERROR:
GOFILES := $(shell find . -name '*.go' -print)

all: deps build test
build: 
		$(VGO) build -ldflags "-X main.buildDate=`date -u +\"%Y-%m-%dT%H:%M:%SZ\"` -X main.buildVersion=$(BUILD_VERSION)" -tags=prod -o $(BINARY_NAME) -v
coverage.txt: $(GOFILES)
		$(VGO) test  ./... -cover -coverprofile=coverage.txt -covermode=atomic
test: coverage.txt
clean: 
		$(VGO) clean
		rm -f coverage.txt
		rm -f $(BINARY_NAME)
		rm -f $(BINARY_UNIX)
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

