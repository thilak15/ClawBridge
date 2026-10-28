.PHONY: all build build-all test clean install

all: build test

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="-s -w" -trimpath -o bin/clawbridge ./cmd/clawbridge

build-all:
	@./scripts/build.sh

test:
	CGO_ENABLED=0 go test -v ./...

clean:
	@rm -rf bin dist

install: build
	@./scripts/install.sh

