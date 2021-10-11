VGO=go # Set to vgo if building in Go 1.10
BINARY_NAME=ethconnect
BINARY_UNIX=$(BINARY_NAME)-tux
BINARY_MAC=$(BINARY_NAME)-mac
BINARY_WIN=$(BINARY_NAME)-win
TEST_DEBUG_FLAGS?=

GOBIN := $(shell $(VGO) env GOPATH)/bin
MOCKERY := $(GOBIN)/mockery

.DELETE_ON_ERROR:
GOFILES := $(shell find . -name '*.go' -print)

all: deps build test
build: ethbinding.so
		$(VGO) build -ldflags "-X main.buildDate=`date -u +\"%Y-%m-%dT%H:%M:%SZ\"` -X main.buildVersion=$(BUILD_VERSION)" -tags=prod -o $(BINARY_NAME) -v
ethbinding.so:
	  go build -buildmode=plugin github.com/kaleido-io/ethbinding
clean-ethbinding: force
	  rm ethbinding.so
reset-ethbinding: clean-ethbinding ethbinding.so ;
delv-ethbinding: force
# if using delv or vscode to debug, use "make delv-ethbinding", otherwise you will get error:
# "plugin was built with a different version of package runtime/internal/sys"
	  go build -buildmode=plugin -gcflags='all=-N -l' github.com/kaleido-io/ethbinding
coverage.txt: $(GOFILES)
		$(VGO) test  ./... ${TEST_DEBUG_FLAGS} -cover -coverprofile=coverage.txt -covermode=atomic -timeout 30s
coverage.html:
	  $(VGO) tool cover -html=coverage.txt
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

${MOCKERY}:
		$(VGO) install github.com/vektra/mockery/cmd/mockery@latest
sarama:
		$(eval SARAMA_PATH := $(shell $(VGO) list -f '{{.Dir}}' github.com/Shopify/sarama))

define makemock
mocks: mocks-$(strip $(1))-$(strip $(2))
mocks-$(strip $(1))-$(strip $(2)): ${MOCKERY} sarama
	${MOCKERY} --case underscore --dir $(1) --name $(2) --outpkg $(3) --output mocks/$(strip $(3))
endef

$(eval $(call makemock, internal/contractregistry, ContractStore,        contractregistrymocks))
$(eval $(call makemock, internal/contractregistry, RemoteRegistry,       contractregistrymocks))
$(eval $(call makemock, internal/eth,              RPCClient,            ethmocks))
$(eval $(call makemock, $$(SARAMA_PATH),           Client,               saramamocks))
$(eval $(call makemock, $$(SARAMA_PATH),           ConsumerGroup,        saramamocks))
$(eval $(call makemock, $$(SARAMA_PATH),           ConsumerGroupSession, saramamocks))
$(eval $(call makemock, $$(SARAMA_PATH),           ConsumerGroupClaim,   saramamocks))
