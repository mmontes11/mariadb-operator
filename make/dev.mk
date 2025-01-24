MARIADB_OPERATOR_NAME ?= mariadb-operator
MARIADB_OPERATOR_NAMESPACE ?= default
MARIADB_OPERATOR_SA_PATH ?= /tmp/mariadb-operator/token
WATCH_NAMESPACE ?= ""
KUBEBUILDER_ASSETS ?= "$(shell $(ENVTEST) use $(KUBERNETES_VERSION) -p path)"

ENV ?= \
	RELATED_IMAGE_MARIADB=$(RELATED_IMAGE_MARIADB) \
	RELATED_IMAGE_MAXSCALE=$(RELATED_IMAGE_MAXSCALE) \
	RELATED_IMAGE_EXPORTER=$(RELATED_IMAGE_EXPORTER) \
	RELATED_IMAGE_EXPORTER_MAXSCALE=$(RELATED_IMAGE_EXPORTER_MAXSCALE) \
	MARIADB_GALERA_LIB_PATH=$(MARIADB_GALERA_LIB_PATH) \
	MARIADB_OPERATOR_IMAGE=$(IMG) \
	MARIADB_OPERATOR_NAME=$(MARIADB_OPERATOR_NAME) \
	MARIADB_OPERATOR_NAMESPACE=$(MARIADB_OPERATOR_NAMESPACE) \
	MARIADB_OPERATOR_SA_PATH=$(MARIADB_OPERATOR_SA_PATH) \
	MARIADB_ENTRYPOINT_VERSION=$(MARIADB_ENTRYPOINT_VERSION) \
	WATCH_NAMESPACE=$(WATCH_NAMESPACE) \
	KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS)

ENV_ENT ?= \
	RELATED_IMAGE_MARIADB=$(RELATED_IMAGE_MARIADB_ENT) \
	RELATED_IMAGE_MAXSCALE=$(RELATED_IMAGE_MAXSCALE_ENT) \
	RELATED_IMAGE_EXPORTER=$(RELATED_IMAGE_EXPORTER_ENT) \
	RELATED_IMAGE_EXPORTER_MAXSCALE=$(RELATED_IMAGE_EXPORTER_MAXSCALE_ENT) \
	MARIADB_GALERA_LIB_PATH=$(MARIADB_GALERA_LIB_PATH_ENT) \
	MARIADB_OPERATOR_IMAGE=$(IMG_ENT) \
	MARIADB_OPERATOR_NAME=$(MARIADB_OPERATOR_NAME) \
	MARIADB_OPERATOR_NAMESPACE=$(MARIADB_OPERATOR_NAMESPACE) \
	MARIADB_OPERATOR_SA_PATH=$(MARIADB_OPERATOR_SA_PATH) \
	MARIADB_ENTRYPOINT_VERSION=$(MARIADB_ENTRYPOINT_VERSION_ENT) \
	WATCH_NAMESPACE=$(WATCH_NAMESPACE) \
	TEST_ENTERPRISE=true \
	KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS)

TEST_ARGS ?= --coverprofile=cover.out --timeout 30m -p
TEST ?= $(ENV) $(GINKGO) $(TEST_ARGS)
TEST_ENT ?= $(ENV_ENT) $(GINKGO) $(TEST_ARGS)

GOCOVERDIR ?= .

##@ Test

.PHONY: test
test: envtest ginkgo ## Run unit tests.
	$(TEST) ./pkg/... ./api/... ./internal/helmtest/...

test-helm: envtest ginkgo ## Run helm unit tests.
	$(TEST) ./internal/helmtest/...

.PHONY: test-int
test-int: envtest ginkgo ## Run integration tests.
	$(TEST) ./internal/controller/...

.PHONY: test-int-ent
test-int-ent: envtest ginkgo ## Run enterprise integration tests.
	$(TEST_ENT) ./internal/controller/...

.PHONY: cover
cover: ## Generate and view coverage report.
	@$(GO) tool cover -html=cover.out -o=cover.html
	open cover.html

##@ Lint

.PHONY: lint
lint: golangci-lint ## Lint.
	$(GOLANGCI_LINT) run

##@ Release

.PHONY: release
release: goreleaser ## Test release locally.
	$(GORELEASER) release --snapshot --clean

##@ Run

RUN_FLAGS ?= --log-dev --log-level=info --log-time-encoder=iso8601

.PHONY: run
run: lint ## Run a controller from your host.
	$(ENV) $(GO) run cmd/controller/*.go $(RUN_FLAGS)

.PHONY: run-ent
run-ent: lint cert ## Run a enterprise controllers from your host.
	$(ENV_ENT) $(GO) run cmd/enterprise/*.go $(RUN_FLAGS)

WEBHOOK_FLAGS ?= --log-dev --log-level=debug --log-time-encoder=iso8601 \
	--ca-cert-path=$(CA_DIR)/tls.crt --cert-dir=$(CERT_DIR) \
	--validate-cert=false
.PHONY: webhook
webhook: lint cert ## Run a webhook from your host.
	$(GO) run cmd/controller/*.go webhook $(WEBHOOK_FLAGS)

CERT_CONTROLLER_FLAGS ?= --log-dev --log-level=debug --log-time-encoder=iso8601 \
	--ca-validity=24h --cert-validity=1h --lookahead-validity=8h --requeue-duration=1m
.PHONY: cert-controller
cert-controller: lint ## Run a cert-controller from your host.
	$(GO) run cmd/controller/*.go cert-controller $(CERT_CONTROLLER_FLAGS)

BACKUP_ENV ?= AWS_ACCESS_KEY_ID=mariadb-operator AWS_SECRET_ACCESS_KEY=Minio11!
BACKUP_COMMON_FLAGS ?= --path=backup --target-file-path=backup/0-backup-target.txt \
	--s3 --s3-bucket=backups --s3-endpoint=minio:9000 --s3-region=us-east-1 --s3-tls --s3-ca-cert-path=/tmp/certificate-authority/tls.crt \
	--compression=gzip --log-dev --log-level=debug --log-time-encoder=iso8601

BACKUP_FLAGS ?= --max-retention=8h $(BACKUP_COMMON_FLAGS)
.PHONY: backup
backup: lint ## Run backup from your host.
	$(BACKUP_ENV) $(GO) run cmd/controller/*.go backup $(BACKUP_FLAGS)

RESTORE_FLAGS ?= --target-time=1970-01-01T00:00:00Z $(BACKUP_COMMON_FLAGS)
.PHONY: restore
restore: lint ## Run restore from your host.
	$(BACKUP_ENV) $(GO) run cmd/controller/*.go backup restore $(RESTORE_FLAGS)

.PHONY: local-dir
local-dir: ## Create config and state directories for local development.
	mkdir -p mariadb/config
	mkdir -p mariadb/state

POD_ENV ?= \
	CLUSTER_NAME=cluster.local  \
	POD_NAME=mariadb-galera-0 \
	POD_NAMESPACE=default \
	POD_IP=10.244.0.36  \
	MARIADB_NAME=mariadb-galera \
	MARIADB_ROOT_PASSWORD=MariaDB11! \
	MYSQL_TCP_PORT=3306 \
	KUBECONFIG=$(HOME)/.kube/config

INIT_FLAGS ?= $(RUN_FLAGS) --config-dir=mariadb/config --state-dir=mariadb/state
.PHONY: init
init: local-dir ## Run init from your host.
	$(POD_ENV) $(GO) run cmd/controller/*.go init $(INIT_FLAGS)

# AGENT_AUTH_FLAGS ?= --kubernetes-auth=true --kubernetes-trusted-name=mariadb-galera --kubernetes-trusted-namespace=default
AGENT_AUTH_FLAGS ?=
AGENT_FLAGS ?= $(RUN_FLAGS) $(AGENT_AUTH_FLAGS) --config-dir=mariadb/config --state-dir=mariadb/state
.PHONY: agent
agent: local-dir ## Run agent from your host.
	$(POD_ENV) $(GO) run cmd/controller/*.go agent $(AGENT_FLAGS)
