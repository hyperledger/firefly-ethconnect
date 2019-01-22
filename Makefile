 # Go parameters
VGO=vgo
BINARY_NAME=ethconnect
BINARY_UNIX=$(BINARY_NAME)-tux
BINARY_MAC=$(BINARY_NAME)-mac
BINARY_WIN=$(BINARY_NAME)-win

all: deps build test
build: 
		$(VGO) build -ldflags "-X main.buildDate=`date -u +\"%Y-%m-%dT%H:%M:%SZ\"` -X main.buildVersion=$(BUILD_VERSION)" -tags=prod -o $(BINARY_NAME) -v
test:
		$(VGO) test  ./... -cover -coverprofile=coverage.txt -covermode=atomic
clean: 
		$(VGO) clean
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

