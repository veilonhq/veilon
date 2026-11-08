# Veilon build targets.
GO        ?= go
BIN       ?= bin/veilon
PKG       := github.com/veilonhq/veilon
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE      ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -s -w \
	-X $(PKG)/panel/internal/version.Version=$(VERSION) \
	-X $(PKG)/panel/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/panel/internal/version.BuildDate=$(DATE)
WEBUI_DIST := panel/internal/webui/dist
PLATFORMS  := linux/amd64 linux/arm64

.PHONY: all build web web-clean test vet fmt fmt-check release dev clean

all: build

## build: compile the veilon binary for the host
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./panel/cmd/veilon

## web: build the React UI and copy it into the embed directory
web:
	cd web && npm ci && npm run build
	rm -rf $(WEBUI_DIST)
	cp -r web/dist $(WEBUI_DIST)

## web-clean: restore the placeholder UI
web-clean:
	rm -rf $(WEBUI_DIST)
	git checkout -- $(WEBUI_DIST)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w panel

fmt-check:
	@out=$$(gofmt -l panel); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## release: cross-compile linux binaries into dist/ with checksums
release:
	rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/veilon-$$os-$$arch ./panel/cmd/veilon || exit 1; \
	done
	cd dist && sha256sum veilon-* > checksums.txt && cat checksums.txt

## dev: run locally with SQLite in ./.dev
dev: build
	VEILON_CONFIG=/dev/null VEILON_DB_DRIVER=sqlite VEILON_DATA_DIR=$(CURDIR)/.dev \
	VEILON_PANEL_PORT=8080 VEILON_PANEL_PATH=dev VEILON_LISTEN=127.0.0.1 $(BIN) serve

clean:
	rm -rf bin dist .dev
