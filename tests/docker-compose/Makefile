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

# AWS credentials passed to deesql container (works with both local profiles and GHA OIDC).
DEESQL_AWS = \
	-e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY -e AWS_SESSION_TOKEN \
	-e AWS_REGION=$(DSQL_REGION)

DEESQL_DSQL = docker run --rm \
	-v $(CURDIR)/schema:/schema:ro \
	-v $(CURDIR)/cleanup.sql:/cleanup.sql:ro \
	$(DEESQL_AWS) \
	$(IMAGE)

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

# --- Real Aurora DSQL test (deesql CLI only, no psql) ---

test-dsql: build
ifndef DSQL
	$(error DSQL is required. Usage: make test-dsql DSQL=your-cluster.dsql.us-east-1.on.aws)
endif
	@eval $$(aws configure export-credentials --format env $(if $(AWS_PROFILE),--profile $(AWS_PROFILE),) 2>/dev/null) || true; \
	echo "Connecting to $(DSQL) (region: $(DSQL_REGION), user: $(DSQL_USER))"; \
	echo ""; \
	echo "=== Verifying schema ===" && \
	$(DEESQL_DSQL) verify --schema /schema && \
	echo "" && \
	echo "=== Applying schema ===" && \
	$(DEESQL_DSQL) apply \
		--endpoint $(DSQL) \
		--region $(DSQL_REGION) \
		--user $(DSQL_USER) \
		--schema /schema \
		--allow-hazards INDEX_BUILD \
		--force && \
	echo "" && \
	echo "=== Verifying no drift ===" && \
	$(DEESQL_DSQL) plan \
		--endpoint $(DSQL) \
		--region $(DSQL_REGION) \
		--user $(DSQL_USER) \
		--schema /schema && \
	echo "" && \
	echo "=== Cleaning up DSQL ===" && \
	$(DEESQL_DSQL) sql /cleanup.sql \
		--endpoint $(DSQL) \
		--region $(DSQL_REGION) \
		--user $(DSQL_USER) && \
	echo "" && \
	echo "All tests passed on Aurora DSQL."

test: test-docker test-dsql

clean:
	@docker compose down -v 2>/dev/null 2>/dev/null || true
