GOCACHE ?= /tmp/pix-go-cache
GOFLAGS ?= -buildvcs=false

.PHONY: build test clean

build:
	@os="$$(uname -s | tr '[:upper:]' '[:lower:]')"; \
	binary="build/$$os/pix"; \
	mkdir -p "$$(dirname "$$binary")"; \
	GOCACHE=$(GOCACHE) go build $(GOFLAGS) -o "$$binary" ./cmd/pix; \
	if [ "$$(uname -s)" = "Darwin" ]; then \
		codesign --force --sign - --entitlements pix.entitlements "$$binary"; \
	fi; \
	echo "Built $$binary"

test:
	GOCACHE=$(GOCACHE) go test ./...

clean:
	rm -rf build
