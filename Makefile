# Application version encoded in all the binaries.
VERSION = 0.0.0

# Base go module name.
MODULE := $(shell cat go.mod | grep -m1 module | awk '{print $$2}')

# Git revision.
REVISION := $(shell git rev-parse HEAD)

# Some constants to describe the repository.
SRCDIR = src
GENDIR = generated
CRDDIR = charts/core/crds

# Source files defining custom resource APIs
APISRC = $(shell find pkg/apis -name *types.go -type f)

# Some bits about go.
GOPATH := $(shell go env GOPATH)
GOBIN := $(if $(shell go env GOBIN),$(shell go env GOBIN),$(GOPATH)/bin)

# Defines the linter version.
LINT_VERSION=v2.1.5

# Defines the version of the CRD generation tools to use.
CONTROLLER_TOOLS_VERSION=v0.17.3

# Defines the version of code generator tools to use.
# This should be kept in sync with the Kubenetes library versions defined in go.mod.
CODEGEN_VERSION := $(shell grep k8s.io/apimachinery go.mod | awk '{ print $$2; }')

OPENAPI_CODEGEN_VERSION=v2.4.1

OPENAPI_FILES = pkg/openapi/types.go pkg/openapi/schema.go

# Defined the mock generator version.
MOCKGEN_VERSION=v0.3.0

# This is the base directory to generate kubernetes API primitives from e.g.
# clients and CRDs.
GENAPIBASE = github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1 github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/fake github.com/unikorn-cloud/core/pkg/apis/argoproj/v1alpha1

# These are generic arguments that need to be passed to client generation.
GENARGS = --go-header-file hack/boilerplate.go.txt

# This controls the name of the client that will be generated and it will affect
# code import paths.  This overrides the default "versioned".
GENCLIENTNAME = unikorncore

# This defines where clients will be generated.
GENCLIENTS = $(MODULE)/$(GENDIR)/clientset

# Main target, builds all binaries.
.PHONY: all
all: $(GENDIR) $(CRDDIR) $(OPENAPI_FILES)

# TODO: we may wamt to consider porting the rest of the CRD and client generation
# stuff over... that said, we don't need the clients really do we, controller-runtime
# does all the magic for us.
.PHONY: generate
generate:
	@go install go.uber.org/mock/mockgen@$(MOCKGEN_VERSION)
	go generate ./...

.PHONY: test-unit
test-unit:
	go test -coverpkg ./... -coverprofile cover.out ./...
	go tool cover -html cover.out -o cover.html

# Start local Pact Broker
.PHONY: pact-broker-start
pact-broker-start:
	docker-compose -f docker-compose.pact-broker.yml up -d

# Stop local Pact Broker
.PHONY: pact-broker-stop
pact-broker-stop:
	docker-compose -f docker-compose.pact-broker.yml down

# View Pact Broker logs
.PHONY: pact-broker-logs
pact-broker-logs:
	docker-compose -f docker-compose.pact-broker.yml logs -f

# Stop Pact Broker and remove all data
.PHONY: pact-broker-clean
pact-broker-clean:
	docker-compose -f docker-compose.pact-broker.yml down -v

pkg/openapi/types.go: pkg/openapi/common.spec.yaml
	@go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OPENAPI_CODEGEN_VERSION)
	oapi-codegen -generate types,skip-prune -package openapi -o $@ $<

pkg/openapi/schema.go: pkg/openapi/common.spec.yaml
	@go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OPENAPI_CODEGEN_VERSION)
	oapi-codegen -generate spec,skip-prune -package openapi -o $@ $<

# Create any CRDs defined into the target directory.
$(CRDDIR): $(APISRC)
	@mkdir -p $@
	@go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)
	$(GOBIN)/controller-gen crd:crdVersions=v1 paths=./pkg/apis/unikorn/v1alpha1 output:dir=$@
	@touch $(CRDDIR)

# Generate a clientset to interact with our custom resources.
$(GENDIR): $(APISRC)
	@go install k8s.io/code-generator/cmd/deepcopy-gen@$(CODEGEN_VERSION)
	$(GOBIN)/deepcopy-gen --output-file zz_generated.deepcopy.go $(GENARGS) $(GENAPIBASE)
	@touch $@

# When checking out, the files timestamps are pretty much random, and make cause
# spurious rebuilds of generated content.  Call this to prevent that.
.PHONY: touch
touch:
	touch $(CRDDIR) $(GENDIR) pkg/apis/unikorn/v1alpha1/zz_generated.deepcopy.go

# Perform linting.
# This must pass or you will be denied by CI.
.PHOMY: lint
lint: $(GENDIR)
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VERSION)
	$(GOBIN)/golangci-lint run ./...
	helm lint --strict charts/core

# Validate the server OpenAPI schema is legit.
.PHONY: validate
validate: $(OPENAPI_FILES)
	go run ./hack/validate_openapi

# Perform license checking.
# This must pass or you will be denied by CI.
.PHONY: license
license:
	go run ./hack/check_license
