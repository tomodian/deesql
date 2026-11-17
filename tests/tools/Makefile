.PHONY: test clean build

IMAGE = deesql:local
ROOT  = $(CURDIR)/../..

DEESQL = POSTGRES_USER=admin POSTGRES_PASSWORD=password \
	go run $(ROOT)

build:
	docker build -t $(IMAGE) -f ../../images/Dockerfile ../..

test: build clean
	docker compose up -d
	@echo "Waiting for services..."
	@sleep 5
	@echo ""
	@echo "=== verify: check schema files ==="
	@$(DEESQL) verify --schema schema
	@echo ""
	@echo "=== plan: expect changes (empty database) ==="
	@$(DEESQL) plan \
		--endpoint localhost:15432 \
		--schema schema
	@echo ""
	@echo "=== apply: create tables ==="
	@$(DEESQL) apply \
		--endpoint localhost:15432 \
		--schema schema \
		--allow-hazards INDEX_BUILD \
		--force
	@echo ""
	@echo "=== plan: expect no drift ==="
	@$(DEESQL) plan \
		--endpoint localhost:15432 \
		--schema schema | tee /dev/stderr | grep -q "No changes detected"
	@echo ""
	@echo "=== verify: v2 schema ==="
	@$(DEESQL) verify --schema schema_v2
	@echo ""
	@echo "=== apply: migrate to v2 (ALTER paths) ==="
	@$(DEESQL) apply \
		--endpoint localhost:15432 \
		--schema schema_v2 \
		--allow-hazards INDEX_BUILD,INDEX_DROPPED,DELETES_DATA,CORRECTNESS,CONSTRAINT_VALIDATION \
		--force
	@echo ""
	@echo "=== plan: expect no drift after v2 ==="
	@$(DEESQL) plan \
		--endpoint localhost:15432 \
		--schema schema_v2 | tee /dev/stderr | grep -q "No changes detected"
	@echo ""
	@echo "=== sql: run cleanup ==="
	@$(DEESQL) sql cleanup.sql \
		--endpoint localhost:15432
	@echo ""
	@echo "All tools tests passed."
	@docker compose down -v 2>/dev/null

clean:
	@docker compose down -v 2>/dev/null || true
