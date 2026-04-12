.PHONY: build run test test-tools

build:
	go build -o deesql .

run: build
	./deesql $(ARGS)

test:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out
