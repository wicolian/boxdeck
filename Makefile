GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: build release test clean
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o boxdeck .

test:
	$(GO) vet ./...
	$(GO) test ./...

release:
	mkdir -p dist
	@set -e; for os in linux darwin; do \
	  for arch in amd64 arm64; do \
	    echo "Building $$os/$$arch"; \
	    CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o dist/boxdeck_$${os}_$${arch} .; \
	  done; \
	done

clean:
	rm -f boxdeck
	rm -rf dist
