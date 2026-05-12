BINARY := pibox
GOCACHE ?= /tmp/pibox-go-cache
GOFLAGS ?= -buildvcs=false

.PHONY: build test clean

build:
	GOCACHE=$(GOCACHE) go build $(GOFLAGS) -o $(BINARY) ./cmd/pibox

test:
	GOCACHE=$(GOCACHE) go test ./...

clean:
	rm -f $(BINARY)
