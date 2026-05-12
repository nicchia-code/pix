BINARY := pix
GOCACHE ?= /tmp/pibox-go-cache
GOFLAGS ?= -buildvcs=false

.PHONY: build test clean

build:
	GOCACHE=$(GOCACHE) go build $(GOFLAGS) -o $(BINARY) ./cmd/pix
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		codesign --force --sign - --entitlements pix.entitlements $(BINARY); \
	fi

test:
	GOCACHE=$(GOCACHE) go test ./...

clean:
	rm -f $(BINARY)
