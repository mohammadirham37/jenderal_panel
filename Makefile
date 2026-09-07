.PHONY: dev build test lint clean frontend

BINARY=jenderal
VERSION?=0.1.0
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

build: frontend
	rm -rf cmd/jenderal/web_build
	cp -r web/build cmd/jenderal/web_build
	go build $(LDFLAGS) -o $(BINARY) ./cmd/jenderal

dev:
	go run ./cmd/jenderal serve --config configs/jenderal.yaml.example

test:
	go test ./... -v -race

lint:
	go vet ./...
	golangci-lint run

frontend:
	cd web && npm install && npm run build

clean:
	rm -f $(BINARY)
	rm -rf web/build web/node_modules web/.svelte-kit cmd/jenderal/web_build

release: test build
	@echo "Built $(BINARY) v$(VERSION)"
