SHELL := /bin/sh

GO ?= go
HELM ?= helm
CONTROLLER_GEN_VERSION ?= v0.16.5
CONTROLLER_GEN_BIN ?= $(CURDIR)/.cache/bin/controller-gen
IMG ?= ghcr.io/greedykomododragon/cabure
VERSION ?= dev
HELM_RELEASE ?= cabure
NAMESPACE ?= cabure-system
CHART_DIR ?= charts/cabure
GOCACHE_DIR ?= .cache/go-build
GOMODCACHE_DIR ?= .cache/go-mod
GOENV = GOCACHE=$(CURDIR)/$(GOCACHE_DIR) GOMODCACHE=$(CURDIR)/$(GOMODCACHE_DIR)

.PHONY: generate manifests fmt vet test test-e2e lint docker-build docker-push helm-sync helm-lint helm-template install uninstall deploy undeploy

generate:
	$(GOENV) $(GO) generate ./...

manifests:
	@mkdir -p config/crd/bases config/rbac
	@[ -x $(CONTROLLER_GEN_BIN) ] || { $(GOENV) GOBIN=$(CURDIR)/.cache/bin $(GO) install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION); }
	$(CONTROLLER_GEN_BIN) crd paths=./api/... output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN_BIN) rbac:roleName=manager-role paths=./internal/controller/... output:rbac:artifacts:config=config/rbac

fmt:
	gofmt -w ./cmd ./api ./internal

vet:
	$(GOENV) $(GO) vet ./...

test:
	$(GOENV) $(GO) test ./...

test-e2e:
	$(GOENV) $(GO) test ./internal/controller -run TestEnvtest

lint:
	$(GOENV) $(GO) test ./...
	$(HELM) lint $(CHART_DIR)

docker-build:
	docker build -t $(IMG):$(VERSION) .

docker-push:
	docker push $(IMG):$(VERSION)

helm-sync:
	@mkdir -p $(CHART_DIR)/crds $(CHART_DIR)/files
	@cp config/crd/bases/*.yaml $(CHART_DIR)/crds/
	@cp config/rbac/role.yaml $(CHART_DIR)/files/rbac.yaml

helm-lint:
	$(HELM) lint $(CHART_DIR)

helm-template:
	$(HELM) template $(HELM_RELEASE) $(CHART_DIR) --namespace $(NAMESPACE)

install:
	$(HELM) upgrade --install $(HELM_RELEASE) $(CHART_DIR) --namespace $(NAMESPACE) --create-namespace --set image.repository=$(IMG) --set image.tag=$(VERSION)

uninstall:
	$(HELM) uninstall $(HELM_RELEASE) --namespace $(NAMESPACE)

deploy: install

undeploy: uninstall
