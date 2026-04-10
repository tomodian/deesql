IMAGE ?= deesql
TAG   ?= latest

.PHONY: build
build:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t $(IMAGE):$(TAG) \
		-f Dockerfile \
		..
