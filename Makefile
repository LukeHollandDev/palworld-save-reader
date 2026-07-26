override PROJECT_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))

GO ?= go
BINARY ?= $(PROJECT_ROOT)/bin/palworld-save-reader
DIST_DIR ?= $(PROJECT_ROOT)/dist
RELEASE_DIR ?= $(DIST_DIR)/release
VERSION ?= dev

.DEFAULT_GOAL := build

.PHONY: build test ci release-build dist release clean

ci:
	test -z "$$(gofmt -l "$(PROJECT_ROOT)")"
	$(GO) vet ./...
	$(GO) test -race ./...

build:
	mkdir -p "$(dir $(BINARY))"
	$(GO) build -trimpath -o "$(BINARY)" ./cmd/palworld-save-reader

test:
	$(GO) test ./...

release-build:
	@test -n "$(GOOS)" || { echo "GOOS is required"; exit 2; }
	@test -n "$(GOARCH)" || { echo "GOARCH is required"; exit 2; }
	@test -n "$(OUTPUT)" || { echo "OUTPUT is required"; exit 2; }
	mkdir -p "$(dir $(OUTPUT))"
	CGO_ENABLED=0 GOOS="$(GOOS)" GOARCH="$(GOARCH)" \
		$(GO) build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o "$(OUTPUT)" ./cmd/palworld-save-reader

dist:
	$(MAKE) release-build GOOS=linux GOARCH=amd64 OUTPUT="$(DIST_DIR)/palworld-save-reader-linux-amd64"
	$(MAKE) release-build GOOS=linux GOARCH=arm64 OUTPUT="$(DIST_DIR)/palworld-save-reader-linux-arm64"
	$(MAKE) release-build GOOS=darwin GOARCH=amd64 OUTPUT="$(DIST_DIR)/palworld-save-reader-darwin-amd64"
	$(MAKE) release-build GOOS=darwin GOARCH=arm64 OUTPUT="$(DIST_DIR)/palworld-save-reader-darwin-arm64"
	$(MAKE) release-build GOOS=windows GOARCH=amd64 OUTPUT="$(DIST_DIR)/palworld-save-reader-windows-amd64.exe"

# release turns each cross-compiled executable into a self-contained archive.
# Third-party notices are included beside the binary so users of a downloaded
# archive receive the same licensing material as source users.
release: dist
	rm -rf "$(RELEASE_DIR)"
	mkdir -p "$(RELEASE_DIR)"
	for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64; do \
		name="palworld-save-reader-$(VERSION)-$$target"; \
		stage="$(RELEASE_DIR)/$$name"; \
		mkdir -p "$$stage/LICENSES"; \
		binary="$(DIST_DIR)/palworld-save-reader-$$target"; \
		if test "$$target" = "windows-amd64"; then binary="$$binary.exe"; fi; \
		cp "$$binary" "$$stage/"; \
		cp LICENSE NOTICE "$$stage/"; \
		cp LICENSES/Apache-2.0.txt "$$stage/LICENSES/"; \
		tar -C "$(RELEASE_DIR)" -czf "$(RELEASE_DIR)/$$name.tar.gz" "$$name"; \
		rm -rf "$$stage"; \
	done
	cd "$(RELEASE_DIR)" && shasum -a 256 *.tar.gz > checksums.txt

clean:
	@test -n "$(PROJECT_ROOT)"
	@test "$(PROJECT_ROOT)" != "/"
	@test -f "$(PROJECT_ROOT)/go.mod"
	rm -rf -- "$(PROJECT_ROOT)/bin" "$(PROJECT_ROOT)/dist"
