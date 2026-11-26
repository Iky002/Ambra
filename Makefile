APP_NAME      := ambra
CMD_PKG       := ./cmd/ambra
BIN_DIR       := bin
DIST_DIR      := dist
AMBRA_VERSION ?= v0.10.8
LDFLAGS       ?= -s -w -X github.com/ambra-media/ambra/internal/config.AmbraVersion=$(AMBRA_VERSION)

PLATFORMS  := \
	linux/amd64 \
	linux/arm64 \
	darwin/arm64 \
	windows/amd64

.PHONY: help web build test cover release-all clean

help:
	@echo "Ambra Media Server Makefile targets:"
	@echo "  make web          - Build embedded React Web UI (web/dist)"
	@echo "  make build        - Build local binary to $(BIN_DIR)/$(APP_NAME) (CGO_ENABLED=0)"
	@echo "  make test         - Run all Go unit & integration tests"
	@echo "  make cover        - Run tests with coverage profile and print summary"
	@echo "  make release-all  - Cross-compile for linux/amd64, linux/arm64, darwin/arm64, windows/amd64 into $(DIST_DIR)/ with SHA256SUMS"
	@echo "  make clean        - Remove $(BIN_DIR)/, $(DIST_DIR)/ and coverage.out"

web:
	cd web && (test -d node_modules || npm ci) && npm run build

build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME) $(CMD_PKG)

test:
	go test -count=1 ./...

cover:
	go test -count=1 -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -n 1

release-all:
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)
	@for target in $(PLATFORMS); do \
		GOOS=$${target%/*}; \
		GOARCH=$${target#*/}; \
		EXT=""; \
		if [ "$$GOOS" = "windows" ]; then EXT=".exe"; fi; \
		OUT="$(DIST_DIR)/$(APP_NAME)-$${GOOS}-$${GOARCH}$${EXT}"; \
		echo "Building $$OUT ..."; \
		CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH go build -trimpath -ldflags="$(LDFLAGS)" -o "$$OUT" $(CMD_PKG) || exit 1; \
	done
	@cd $(DIST_DIR) && \
		if command -v sha256sum >/dev/null 2>&1; then \
			sha256sum $(APP_NAME)-* > SHA256SUMS; \
		else \
			shasum -a 256 $(APP_NAME)-* > SHA256SUMS; \
		fi
	@echo "Release artifacts generated in $(DIST_DIR)/:"
	@ls -lh $(DIST_DIR)
	@cat $(DIST_DIR)/SHA256SUMS

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR) coverage.out
