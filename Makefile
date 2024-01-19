
# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.26.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# version settings
RELEASE?=$(shell git rev-parse HEAD)
COMMIT?=$(shell git rev-parse HEAD)
BUILD_DATE?=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
PROJECT?=github.com/falcosecurity/k8s-metacollector

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development
.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -coverprofile cover.out

##@ Build

.PHONY: build
build: fmt vet ## Build manager binary.
	go build -ldflags \
    "-X '${PROJECT}/pkg/version.semVersion=${RELEASE}' \
    -X '${PROJECT}/pkg/version.gitCommit=${COMMIT}' \
    -X '${PROJECT}/pkg/version.buildDate=${BUILD_DATE}'" \
    -o manager .

.PHONY: run
run: fmt vet ## Run a controller from your host.
	go run ./main.go

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: test ## Build docker image with the manager.
	docker build -t ${IMG} -f build/Dockerfile .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

# Install gci if not available
gci:
ifeq (, $(shell which gci))
	@go install github.com/daixiang0/gci@v0.9.0
GCI=$(GOBIN)/gci
else
GCI=$(shell which gci)
endif

# Install addlicense if not available
addlicense:
ifeq (, $(shell which addlicense))
	@go install github.com/google/addlicense@v1.0.0
ADDLICENSE=$(GOBIN)/addlicense
else
ADDLICENSE=$(shell which addlicense)
endif

# Run go fmt against code and add the licence header
fmt: gci addlicense
	go mod tidy
	go fmt ./...
	find . -type f -name '*.go' -a -exec $(GCI) write -s standard -s default -s "prefix(github.com/alacuku/k8s-metadata)" {} \;
	find . -type f -name '*.go' -exec $(ADDLICENSE) -l apache -c "The Falco Authors" -y "$(shell date +%Y)" {} \;

# Install golangci-lint if not available
golangci-lint:
ifeq (, $(shell which golangci-lint))
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.55.2
GOLANGCILINT=$(GOBIN)/golangci-lint
else
GOLANGCILINT=$(shell which golangci-lint)
endif

# It works when called in a branch different than main.
# "--new-from-rev REV Show only new issues created after git revision REV"
lint: golangci-lint
	$(GOLANGCILINT) run --new-from-rev main

# Generate gRPC files
grpc: protoc
	$(PROTOC) --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative metadata/metadata.proto

protoc:
ifeq (, $(shell which protoc))
	@{ \
	PB_REL="https://github.com/protocolbuffers/protobuf/releases" ;\
	version=3.15.8 ;\
	arch=x86_64 ;\
	curl -LO $${PB_REL}/download/v$${version}/protoc-$${version}-linux-$${arch}.zip ;\
	unzip protoc-$${version}-linux-$${arch}.zip -d $${HOME}/.local ;\
	rm protoc-$${version}-linux-$${arch}.zip ;\
	PROTOC_TMP_DIR=$$(mktemp -d) ;\
	cd $$PROTOC_TMP_DIR ;\
	go mod init tmp ;\
	go install google.golang.org/protobuf/cmd/protoc-gen-go ;\
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc ;\
	rm -rf $$PROTOC_TMP_DIR ;\
	}
endif
PROTOC=$(shell which protoc)