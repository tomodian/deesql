.PHONY: test clean build

IMAGE = deesql:local

DEESQL = POSTGRES_USER=admin POSTGRES_PASSWORD=password \
	go tool github.com/tomodian/deesql

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
		--schema schema
	@echo ""
	@echo "=== sql: run cleanup ==="
	@$(DEESQL) sql cleanup.sql \
		--endpoint localhost:15432
	@echo ""
	@echo "All tools tests passed."
	@docker compose down -v 2>/dev/null

clean:
	@docker compose down -v 2>/dev/null || true
