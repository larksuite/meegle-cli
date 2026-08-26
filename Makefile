# ===========================================================================
# Variables
# ===========================================================================

GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
PLATFORMS   := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64 windows/arm64

# meegle version is sourced from the npm package.json.
MEEGLE_VERSION := $(shell node -p "require('./npm/meegle/package.json').version" 2>/dev/null || echo 0.0.0)
MEEGLE_DEV_LDFLAGS     := -s -w -X github.com/larksuite/meegle-cli/cmd.version=$(MEEGLE_VERSION)+$(GIT_COMMIT)
MEEGLE_RELEASE_LDFLAGS := -s -w -X github.com/larksuite/meegle-cli/cmd.version=$(MEEGLE_VERSION)

NPM_BIN := npm/meegle/bin
NOTICES_DIR := third_party_licenses
NOTICES_FILE := THIRD_PARTY_NOTICES.md
GO_LICENSES := $(shell go env GOPATH)/bin/go-licenses
GOLANGCI_LINT := $(shell go env GOPATH)/bin/golangci-lint

.PHONY: setup build test lint lint-license lint-thirdparty notices clean dev run \
        meegle-dev meegle-all meegle-test

# ===========================================================================
# Global
# ===========================================================================

## Bootstrap the dev environment (run once after cloning).
setup:
	git config core.hooksPath .githooks
	@echo "Git hooks configured."

## Build all products. meegle is built here; Makefile.meego appends meego
## when present (internal repo only).
build: meegle-dev

## Run the full test suite.
test:
	go test ./...

## Lint (must use golangci-lint; auto-installs into GOPATH/bin if missing).
## We no longer fall back to 'go vet' — it does not cover funlen / gofmt -s
## and other rules enabled in CI, so local passes that would fail CI have
## happened in the past. Staying aligned with CI is the simplest fix.
lint:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || { \
		echo "golangci-lint not found, installing to $(GOLANGCI_LINT)..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	}
	$(GOLANGCI_LINT) run ./...

## Verify every source file has the MIT license header (used by CI as a gate).
lint-license:
	go run github.com/google/addlicense@latest -f .license-header.txt -check \
		-ignore 'ref_skills/**' -ignore 'npm/**' -ignore 'vendor/**' \
		-ignore 'node_modules/**' -ignore '.idea/**' \
		-ignore '.claude/**' \
		-ignore '**/*.json' -ignore '**/*.md' -ignore '**/*.yaml' -ignore '**/*.yml' \
		-ignore 'Makefile' -ignore 'Makefile.meego' -ignore 'go.mod' -ignore 'go.sum' \
		-ignore '**/*.txt' -ignore 'LICENSE' .

## Check third-party dependencies for forbidden / restricted licenses (CI gate).
lint-thirdparty:
	@command -v $(GO_LICENSES) >/dev/null 2>&1 || go install github.com/google/go-licenses@latest
	$(GO_LICENSES) check ./cmd/meegle --disallowed_types=forbidden,restricted

## Regenerate third_party_licenses/ (run after go.mod changes).
notices: lint-thirdparty
	@command -v $(GO_LICENSES) >/dev/null 2>&1 || go install github.com/google/go-licenses@latest
	rm -rf $(NOTICES_DIR)
	$(GO_LICENSES) save ./cmd/meegle --save_path=$(NOTICES_DIR) --force
	@echo
	@echo "Refreshed $(NOTICES_DIR)/. Manually update component table in $(NOTICES_FILE) if dependencies changed."

## Remove build artifacts.
clean:
	rm -rf dist/
	rm -f $(NPM_BIN)/meegle-*

## Build the meegle dev binary and print --help.
## Note: this project is a pure CLI — no server, no port. To pass arguments use
## 'go run ./cmd/meegle <args>' or './dist/meegle <args>' (after 'make meegle-dev').
dev: meegle-dev
	./dist/meegle --help

## Alias for 'dev' (matches the muscle memory of many developers).
run: dev

# ===========================================================================
# meegle product
# ===========================================================================

## Build meegle (dev build, includes the git commit hash).
meegle-dev:
	go build -ldflags="$(MEEGLE_DEV_LDFLAGS)" -o dist/meegle ./cmd/meegle

## Cross-compile meegle for all platforms into npm/meegle/bin/ (release build).
meegle-all:
	@for platform in $(PLATFORMS); do \
		goos=$${platform%/*}; \
		goarch=$${platform#*/}; \
		ext=""; name_os=$$goos; \
		if [ "$$goos" = "windows" ]; then ext=".exe"; name_os="win32"; fi; \
		GOOS=$$goos GOARCH=$$goarch \
		go build -ldflags="$(MEEGLE_RELEASE_LDFLAGS)" \
		-o $(NPM_BIN)/meegle-$$name_os-$$(echo $$goarch | sed 's/amd64/x64/')$$ext \
		./cmd/meegle; \
	done
	cp LICENSE npm/meegle/LICENSE

## Run meegle unit tests.
meegle-test:
	go test ./internal/products/meegle/...


