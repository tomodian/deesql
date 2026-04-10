.PHONY: test-docker test-psql test-dsql clean build

IMAGE = deesql:local

build:
	docker build -t $(IMAGE) -f ../../images/Dockerfile ../..

# Local test: psql runs inside the postgres container, connecting through the deesql proxy.
PSQL = docker exec -i docker-compose-postgres-1 psql -h docker-compose-deesql-1 -p 5432 -U admin -d postgres

# DSQL connection settings (override with: make test-dsql DSQL=your-cluster.dsql.us-east-1.on.aws)
DSQL         ?=
AWS_PROFILE  ?= default
DSQL_USER    ?= admin
DSQL_DB      ?= postgres
DSQL_REGION  ?= $(shell echo $(DSQL) | sed -n 's/.*\.dsql\.\(.*\)\.on\.aws/\1/p')

PGPASS_FILE = $(CURDIR)/.pgpass
DSQL_PSQL = docker run --rm -i \
	-v $(CURDIR)/schema:/schema:ro \
	-v $(CURDIR)/test_queries.sql:/test_queries.sql:ro \
	-v $(CURDIR)/cleanup.sql:/cleanup.sql:ro \
	-v $(PGPASS_FILE):/home/.pgpass:ro \
	-e PGPASSFILE=/home/.pgpass \
	postgres:17-alpine \
	psql "host=$(DSQL) port=5432 user=$(DSQL_USER) dbname=$(DSQL_DB) sslmode=require"

# --- Local proxy test ---

test-docker: build clean
	docker compose up -d
	@echo "Waiting for services..."
	@sleep 5
	@echo ""
	@echo "=== Verifying schema ==="
	@docker run --rm \
		-v $(CURDIR)/schema:/schema:ro \
		$(IMAGE) verify --schema /schema
	@echo ""
	@echo "=== Applying schema ==="
	@for f in schema/*.sql; do \
		echo "--> $$f"; \
		cat "$$f" | $(PSQL) 2>/dev/null 2>&1 || exit 1; \
	done
	@echo ""
	@echo "=== Running test queries ==="
	@cat test_queries.sql | $(PSQL) 2>&1 || exit 1
	@echo ""
	@echo "All tests passed."
	@docker compose down -v 2>/dev/null

# --- Real Aurora DSQL test ---

test-dsql: build
ifndef DSQL
	$(error DSQL is required. Usage: make test-dsql DSQL=your-cluster.dsql.us-east-1.on.aws)
endif
	@aws dsql generate-db-connect-admin-auth-token \
		--hostname $(DSQL) --region $(DSQL_REGION) \
		$(if $(AWS_PROFILE),--profile $(AWS_PROFILE),) \
		| xargs -I{} sh -c 'printf "%s:%s:%s:%s:%s\n" "$(DSQL)" "5432" "$(DSQL_DB)" "$(DSQL_USER)" "$$1"' _ {} > $(PGPASS_FILE)
	@chmod 600 $(PGPASS_FILE)
	@echo "Connecting to $(DSQL) (region: $(DSQL_REGION), user: $(DSQL_USER))"
	@echo ""
	@echo "=== Verifying schema ==="
	@docker run --rm \
		-v $(CURDIR)/schema:/schema:ro \
		$(IMAGE) verify --schema /schema
	@echo ""
	@echo "=== Applying schema ==="
	@eval $$(aws configure export-credentials --format env $(if $(AWS_PROFILE),--profile $(AWS_PROFILE),) 2>/dev/null) && \
	docker run --rm \
		-v $(CURDIR)/schema:/schema:ro \
		-e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY -e AWS_SESSION_TOKEN \
		-e AWS_REGION=$(DSQL_REGION) \
		$(IMAGE) apply \
			--endpoint $(DSQL) \
			--region $(DSQL_REGION) \
			--user $(DSQL_USER) \
			--schema /schema \
			--allow-hazards INDEX_BUILD \
			--force
	@echo ""
	@echo "=== Running test queries ==="
	@$(DSQL_PSQL) -f /test_queries.sql 2>&1 || exit 1
	@echo ""
	@echo "=== Cleaning up DSQL ==="
	@$(DSQL_PSQL) -f /cleanup.sql 2>&1
	@echo ""
	@echo "All tests passed on Aurora DSQL."

test: test-docker test-dsql

clean:
	@docker compose down -v 2>/dev/null 2>/dev/null || true
	@rm -f $(PGPASS_FILE)
