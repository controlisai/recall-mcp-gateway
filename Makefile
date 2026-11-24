BINARY := recall-mcp-gateway
PKG := github.com/controlisai/recall-mcp-gateway
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
CHART_DIR := charts/recall-mcp-gateway
DIST_DIR := dist
PLATFORMS := darwin-amd64 darwin-arm64 linux-amd64 linux-arm64 windows-amd64

.PHONY: build test test-integration lint clean docker-build docker-up docker-up-postgres docker-up-multinode run helm-package helm-lint build-all package checksums web-install web-dev web-build web-typecheck build-full

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/gateway/

test:
	go test -race -count=1 ./internal/...

test-integration:
	go test -race -count=1 ./test/integration/...

test-all: test test-integration

lint:
	golangci-lint run ./...

vuln-check:
	go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

trivy-scan: docker-build
	trivy image --severity HIGH,CRITICAL --exit-code 1 $(BINARY):$(VERSION)

clean:
	rm -f $(BINARY)

# Docker
docker-build:
	docker build -t $(BINARY):$(VERSION) -t $(BINARY):latest .

docker-up:
	docker compose --profile standalone up -d

docker-up-postgres:
	docker compose --profile postgres up -d

docker-up-multinode:
	docker compose --profile multinode up -d

docker-down:
	docker compose --profile standalone --profile postgres --profile multinode down

# Helm
helm-lint:
	helm lint $(CHART_DIR)

helm-package:
	helm package $(CHART_DIR) --version $(VERSION) --app-version $(VERSION)

helm-template:
	helm template recall-mcp-gateway $(CHART_DIR)

# Run locally
run:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/gateway/
	./$(BINARY) --config configs/standalone.yaml

run-production:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/gateway/
	./$(BINARY) --config configs/postgres.yaml

# Cross-compile for all platforms
build-all: clean-dist
	@mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-darwin-amd64 ./cmd/gateway/
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-darwin-arm64 ./cmd/gateway/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-linux-amd64 ./cmd/gateway/
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-linux-arm64 ./cmd/gateway/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY)-windows-amd64.exe ./cmd/gateway/
	@echo "Built $(VERSION) for all platforms in $(DIST_DIR)/"

# Package binaries into tarballs
package: build-all
	@cd $(DIST_DIR) && for f in $(BINARY)-darwin-* $(BINARY)-linux-*; do tar -czf $$f.tar.gz $$f; done
	@cd $(DIST_DIR) && zip -q $(BINARY)-windows-amd64.zip $(BINARY)-windows-amd64.exe
	@echo "Packaged archives in $(DIST_DIR)/"

# Generate checksums
checksums: package
	@cd $(DIST_DIR) && shasum -a 256 *.tar.gz *.zip > checksums.txt
	@echo "Checksums written to $(DIST_DIR)/checksums.txt"

# Write latest version file
version-file:
	@echo "$(VERSION)" > $(DIST_DIR)/latest.txt

# Full release: build + package + checksums + version
release: checksums version-file
	@echo "Release $(VERSION) ready in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/*.tar.gz $(DIST_DIR)/*.zip $(DIST_DIR)/checksums.txt $(DIST_DIR)/latest.txt

clean-dist:
	@rm -rf $(DIST_DIR)

# React dashboard v2
web-install:
	cd web && npm install

web-dev:
	cd web && npm run dev

web-build:
	cd web && npm run build

web-typecheck:
	cd web && npm run typecheck

build-full: web-build build
