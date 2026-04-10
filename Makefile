.PHONY: build run test

build:
	go build -o dsql-migrate .

run: build
	./dsql-migrate $(ARGS)

test:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out
