override PROJECT_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))

GO ?= go
BINARY ?= $(PROJECT_ROOT)/bin/savedecode
DIST_DIR ?= $(PROJECT_ROOT)/dist

.DEFAULT_GOAL := build

.PHONY: build test ci release-build dist clean

ci:
	test -z "$$(gofmt -l "$(PROJECT_ROOT)/cmd" "$(PROJECT_ROOT)/internal")"
	$(GO) vet ./...
	$(GO) test -race ./...

build:
	mkdir -p "$(dir $(BINARY))"
	$(GO) build -trimpath -o "$(BINARY)" ./cmd/savedecode

test:
	$(GO) test ./...

release-build:
	@test -n "$(GOOS)" || { echo "GOOS is required"; exit 2; }
	@test -n "$(GOARCH)" || { echo "GOARCH is required"; exit 2; }
	@test -n "$(OUTPUT)" || { echo "OUTPUT is required"; exit 2; }
	mkdir -p "$(dir $(OUTPUT))"
	CGO_ENABLED=0 GOOS="$(GOOS)" GOARCH="$(GOARCH)" \
		$(GO) build -trimpath -ldflags="-s -w" -o "$(OUTPUT)" ./cmd/savedecode

dist:
	$(MAKE) release-build GOOS=linux GOARCH=amd64 OUTPUT="$(DIST_DIR)/savedecode-linux-amd64"
	$(MAKE) release-build GOOS=linux GOARCH=arm64 OUTPUT="$(DIST_DIR)/savedecode-linux-arm64"
	$(MAKE) release-build GOOS=darwin GOARCH=amd64 OUTPUT="$(DIST_DIR)/savedecode-darwin-amd64"
	$(MAKE) release-build GOOS=darwin GOARCH=arm64 OUTPUT="$(DIST_DIR)/savedecode-darwin-arm64"
	$(MAKE) release-build GOOS=windows GOARCH=amd64 OUTPUT="$(DIST_DIR)/savedecode-windows-amd64.exe"

clean:
	@test -n "$(PROJECT_ROOT)"
	@test "$(PROJECT_ROOT)" != "/"
	@test -f "$(PROJECT_ROOT)/go.mod"
	rm -rf -- "$(PROJECT_ROOT)/bin" "$(PROJECT_ROOT)/dist"
