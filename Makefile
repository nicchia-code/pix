BINARY := build/$(shell uname -s | tr '[:upper:]' '[:lower:]')/pix
GOCACHE ?= /tmp/pibox-go-cache
GOFLAGS ?= -buildvcs=false

.PHONY: build test clean

build:
	@mkdir -p $(dir $(BINARY))
	GOCACHE=$(GOCACHE) go build $(GOFLAGS) -o $(BINARY) ./cmd/pix
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		codesign --force --sign - --entitlements pix.entitlements $(BINARY); \
	fi

test:
	GOCACHE=$(GOCACHE) go test ./...

clean:
	rm -rf build
