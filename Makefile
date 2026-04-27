.PHONY: all build test test-race vet lint vuln sec audit tidy clean dist dist-linux dist-darwin dist-darwin-arm64 dist-windows

GO ?= go
GOBIN ?= $(shell $(GO) env GOPATH)/bin

all: build test audit

build:
	$(GO) build ./...

test:
	$(GO) test -race ./...

test-race: test

vet:
	$(GO) vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@PATH="$(GOBIN):$$PATH" golangci-lint run ./...

vuln:
	@command -v govulncheck >/dev/null 2>&1 || $(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	@PATH="$(GOBIN):$$PATH" govulncheck ./...

sec:
	@command -v gosec >/dev/null 2>&1 || $(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	@PATH="$(GOBIN):$$PATH" gosec ./...

audit: lint vuln sec

tidy:
	$(GO) mod tidy

clean:
	rm -rf bin dist build

# ---- Distribution packaging ------------------------------------------------

DIST_DIR   ?= dist
APP_NAME   ?= goremote
# Version from git describe; falls back to "dev" outside a git repo.
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# dist-linux: builds the linux/amd64 binary and packages it as a tar.gz.
# Output: dist/goremote-$(VERSION)-linux-amd64.tar.gz
dist-linux:
	@mkdir -p $(DIST_DIR)/linux-amd64
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 $(GO) build -ldflags "-X main.Version=$(VERSION)" \
		-o $(DIST_DIR)/linux-amd64/$(APP_NAME) ./cmd/desktop
	tar -czf $(DIST_DIR)/$(APP_NAME)-$(VERSION)-linux-amd64.tar.gz \
		-C $(DIST_DIR)/linux-amd64 $(APP_NAME)
	@echo "Created $(DIST_DIR)/$(APP_NAME)-$(VERSION)-linux-amd64.tar.gz"

# dist-darwin: builds the darwin/amd64 binary and packages it as a zip.
# Output: dist/goremote-$(VERSION)-darwin-amd64.zip
dist-darwin:
	@mkdir -p $(DIST_DIR)/darwin-amd64
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 $(GO) build -ldflags "-X main.Version=$(VERSION)" \
		-o $(DIST_DIR)/darwin-amd64/$(APP_NAME) ./cmd/desktop
	cd $(DIST_DIR)/darwin-amd64 && zip -q ../$(APP_NAME)-$(VERSION)-darwin-amd64.zip $(APP_NAME)
	@echo "Created $(DIST_DIR)/$(APP_NAME)-$(VERSION)-darwin-amd64.zip"

# dist-darwin-arm64: builds the darwin/arm64 (Apple Silicon) binary.
# Output: dist/goremote-$(VERSION)-darwin-arm64.zip
dist-darwin-arm64:
	@mkdir -p $(DIST_DIR)/darwin-arm64
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "-X main.Version=$(VERSION)" \
		-o $(DIST_DIR)/darwin-arm64/$(APP_NAME) ./cmd/desktop
	cd $(DIST_DIR)/darwin-arm64 && zip -q ../$(APP_NAME)-$(VERSION)-darwin-arm64.zip $(APP_NAME)
	@echo "Created $(DIST_DIR)/$(APP_NAME)-$(VERSION)-darwin-arm64.zip"

# dist-windows: builds the windows/amd64 binary plus a launcher .bat file.
# Output: dist/goremote-$(VERSION)-windows-amd64.zip
dist-windows:
	@mkdir -p $(DIST_DIR)/windows-amd64
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc $(GO) build -ldflags "-X main.Version=$(VERSION)" \
		-o $(DIST_DIR)/windows-amd64/$(APP_NAME).exe ./cmd/desktop
	printf '@echo off\r\nstart "" "%%~dp0$(APP_NAME).exe" %%*\r\n' \
		> $(DIST_DIR)/windows-amd64/$(APP_NAME).bat
	cd $(DIST_DIR)/windows-amd64 && zip -q ../$(APP_NAME)-$(VERSION)-windows-amd64.zip \
		$(APP_NAME).exe $(APP_NAME).bat
	@echo "Created $(DIST_DIR)/$(APP_NAME)-$(VERSION)-windows-amd64.zip"

# dist: build all distribution packages.
dist: dist-linux dist-darwin dist-darwin-arm64 dist-windows
