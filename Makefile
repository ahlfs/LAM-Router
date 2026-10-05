BINARY_NAME := lam-router
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "1.8.4")
PORT ?= 9898
DATA_DIR ?= $(HOME)/.lam-router
RTK ?=
CAVEMAN ?=
PONYTAIL ?=
AUTO_UPDATE ?= false

LDFLAGS := -s -w -X 'lamrouter/internal/updater.CurrentVersion=$(VERSION)'

.PHONY: build install run dev version update test test-short vet bench bench-go cross mitm-enable mitm-disable mitm-status docker docker-build clean help

## build — compile binary with embedded webdist assets
build:
	go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/lamrouter/

## build-ui — build web frontend and sync assets to internal/webdist/dist
build-ui:
	@if [ -d "../LAM-Router-staging/apps/web" ]; then \
		cd ../LAM-Router-staging/apps/web && pnpm run build && \
		rm -rf internal/webdist/dist/* && \
		cp -r ../LAM-Router-staging/apps/web/dist/* internal/webdist/dist/ && \
		cp -r ../LAM-Router-staging/apps/web/dist/* web/dist/ 2>/dev/null || true; \
	fi

## install — compile and install globally into $GOPATH/bin or /usr/local/bin
install: build
	@if [ -w /usr/local/bin ]; then \
		cp -f bin/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME); \
		cp -f bin/$(BINARY_NAME) /usr/local/bin/lamrouter 2>/dev/null || true; \
		echo "✅ Installed globally to /usr/local/bin! You can now run 'lam-router' or 'lamrouter' anywhere."; \
	elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then \
		sudo cp -f bin/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME); \
		sudo cp -f bin/$(BINARY_NAME) /usr/local/bin/lamrouter 2>/dev/null || true; \
		echo "✅ Installed globally to /usr/local/bin via sudo! You can now run 'lam-router' or 'lamrouter' anywhere."; \
	else \
		mkdir -p $(HOME)/.local/bin; \
		cp -f bin/$(BINARY_NAME) $(HOME)/.local/bin/$(BINARY_NAME); \
		cp -f bin/$(BINARY_NAME) $(HOME)/.local/bin/lamrouter 2>/dev/null || true; \
		if ! echo "$$PATH" | grep -q "$(HOME)/.local/bin"; then \
			echo 'export PATH="$$HOME/.local/bin:$$PATH"' >> $(HOME)/.bashrc; \
			echo "⚠️  Added ~/.local/bin to ~/.bashrc. Please run 'source ~/.bashrc' or reload your shell."; \
		fi; \
		echo "✅ Installed to ~/.local/bin! You can now run 'lam-router' or 'lamrouter'."; \
	fi

## run — start proxy (PORT=20128)
run: build
	PORT=$(PORT) DATA_DIR=$(DATA_DIR) ./$(BINARY_NAME) $(if $(RTK),--rtk=$(RTK)) $(if $(CAVEMAN),--caveman=$(CAVEMAN)) $(if $(PONYTAIL),--ponytail=$(PONYTAIL)) --auto-update=$(AUTO_UPDATE)

## dev — start with go run (auto-rebuild)
dev:
	PORT=$(PORT) DATA_DIR=$(DATA_DIR) go run -ldflags="$(LDFLAGS)" ./cmd/lamrouter/ $(if $(RTK),--rtk=$(RTK)) $(if $(CAVEMAN),--caveman=$(CAVEMAN)) $(if $(PONYTAIL),--ponytail=$(PONYTAIL)) --auto-update=$(AUTO_UPDATE)

## version — print binary version
version:
	@echo "$(VERSION)"

## update — self-update binary
update:
	./bin/$(BINARY_NAME) update

## test — run all unit and integration tests
test:
	go test -v ./...

## test-short — run unit tests only (skip slow/e2e tests)
test-short:
	go test -short ./...

## vet — run go vet
vet:
	go vet ./...

## bench — run all benchmarks with memory allocation metrics
bench:
	go test -bench=. -benchmem -run=^$$ ./internal/handlers/chat/ ./internal/proxy/

## bench-go — run standalone high-concurrency Go proxy benchmark runner
bench-go:
	cd benchmark && go run runner.go

## cross — cross-compile for common platforms
cross:
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME)-linux-amd64 ./cmd/lamrouter/
	GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME)-linux-arm64 ./cmd/lamrouter/
	GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME)-darwin-amd64 ./cmd/lamrouter/
	GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME)-darwin-arm64 ./cmd/lamrouter/
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME)-windows-amd64.exe ./cmd/lamrouter/
	@ls -lh $(BINARY_NAME)-*

## mitm-enable — start MITM proxy
mitm-enable: build
	./$(BINARY_NAME) mitm enable

## mitm-disable — stop MITM proxy
mitm-disable: build
	./$(BINARY_NAME) mitm disable

## mitm-status — check MITM proxy status
mitm-status: build
	./$(BINARY_NAME) mitm status

## docker — docker compose up
docker:
	docker compose up -d

## docker-build — build Docker image only
docker-build:
	docker build -t $(BINARY_NAME) .

## clean — remove build artifacts
clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME)-* bin/$(BINARY_NAME) bin/lamrouter npm/*.tgz

## npm-pack — test package npm locally without publishing
npm-pack:
	cd npm && npm pack

## npm-publish — publish package to npmjs.org
npm-publish:
	cd npm && npm publish --access public

## help — show targets
help:
	@echo "lam-router — Makefile targets:"
	@grep -E '^## ' Makefile | sed 's/## /  make /' | sed 's/ — /  /'
	@echo ""
	@echo "Options:"
	@echo "  make run PORT=3000 VERSION=1.1.0"
	@echo "  make run DATA_DIR=/path/to/data"
	@echo "  make run CAVEMAN=true PONYTAIL=true AUTO_UPDATE=true"
	@echo "  make run RTK=false"
