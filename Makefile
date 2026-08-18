.PHONY: test build lint example all
all: lint test build

test:
	go test ./...

build:
	go build -o atlas ./cmd/atlas

lint:
	go vet ./...

example: build
	./examples/run-example.sh
