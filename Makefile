GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: build bar-build bar-release release test clean
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o boxdeck .

bar-build:
	$(GO) -C cmd/boxdeck-bar build -trimpath -ldflags="-s -w" -o ../../boxdeck-bar .

bar-release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) -C cmd/boxdeck-bar build -trimpath -ldflags="-s -w" -o ../../dist/boxdeck-bar_linux_amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) -C cmd/boxdeck-bar build -trimpath -ldflags="-s -w" -o ../../dist/boxdeck-bar_linux_arm64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) -C cmd/boxdeck-bar build -trimpath -ldflags="-s -w -H=windowsgui" -o ../../dist/boxdeck-bar_windows_amd64.exe .

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
