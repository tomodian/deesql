.PHONY: build run

build:
	go build -o dsql-migrate .

run: build
	./dsql-migrate $(ARGS)
