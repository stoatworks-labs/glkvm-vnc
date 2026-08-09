.PHONY: all build frontend test vet clean

all: build

# Build the embedded frontend into web/dist.
frontend:
	cd web/frontend && npm install && npm run build

# Build both binaries (frontend must exist for go:embed).
build: frontend
	mkdir -p bin
	go build -o bin/glkvm-vnc ./cmd/glkvm-vnc
	go build -o bin/glkvm-vnc-agent ./cmd/glkvm-vnc-agent

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf bin web/dist web/frontend/node_modules
