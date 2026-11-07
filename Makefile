# slop — one Makefile for every app and package in the workspace.
#
#   make                  build every app into ./bin
#   make gitmon           build one app
#   make run APP=slash    build and run an app (ARGS="--theme nord")
#   make test             test every module
#   make check            fmt check + vet + test (what CI runs)
#   make install APP=x    install an app into $(PREFIX)/bin
#   make new-app NAME=x   scaffold apps/x wired to the shared packages
#   make dist             cross-compile release archives into ./dist
#   make help             list targets

SHELL   := /bin/sh
APPS    := $(notdir $(wildcard apps/*))
MODULES := $(shell go list -m -f '{{.Dir}}' 2>/dev/null)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BI      := github.com/karim-w/slop/pkg/domain/buildinfo
LDFLAGS := -s -w -X $(BI).Version=$(VERSION) -X $(BI).Commit=$(COMMIT) -X $(BI).Date=$(DATE)

PREFIX    ?= $(HOME)/.local
PLATFORMS ?= darwin/arm64 darwin/amd64 linux/arm64 linux/amd64

.DEFAULT_GOAL := build
.PHONY: build $(APPS) run test test-race vet lint fmt fmt-check check tidy install new-app dist screenshots clean help

build: $(APPS) ## Build every app into ./bin

$(APPS): ## Build one app: make gitmon
	@mkdir -p bin
	cd apps/$@ && go build -trimpath -ldflags '$(LDFLAGS)' -o ../../bin/$@ .
	@echo "built bin/$@"

run: ## Build and run an app: make run APP=gitmon ARGS="--theme nord"
	@test -n "$(APP)" || { echo "usage: make run APP=<$(APPS)>"; exit 1; }
	@$(MAKE) --no-print-directory $(APP)
	./bin/$(APP) $(ARGS)

test: ## Test every module
	@set -e; for m in $(MODULES); do (cd $$m && go test ./...) ; done

test-race: ## Test every module with the race detector
	@set -e; for m in $(MODULES); do (cd $$m && go test -race ./...) ; done

vet: ## go vet every module
	@set -e; for m in $(MODULES); do (cd $$m && go vet ./...) ; done

lint: ## golangci-lint every module (if installed)
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not installed, skipping"; exit 0; }; \
	set -e; for m in $(MODULES); do (cd $$m && golangci-lint run ./...) ; done

fmt: ## Format code and group imports
	gofmt -w apps pkg tools
	@command -v goimports >/dev/null && goimports -local github.com/karim-w/slop -w apps pkg tools || true

fmt-check: ## Fail if any file needs gofmt
	@out=$$(gofmt -l apps pkg tools); test -z "$$out" || { echo "needs gofmt:"; echo "$$out"; exit 1; }

check: fmt-check vet test ## Everything CI runs

tidy: ## go mod tidy every module, then sync the workspace
	@set -e; for m in $(MODULES); do echo "tidy $$m"; (cd $$m && GOWORK=off go mod tidy) ; done
	go work sync

install: ## Install an app: make install APP=slash [PREFIX=~/.local]
	@test -n "$(APP)" || { echo "usage: make install APP=<$(APPS)>"; exit 1; }
	@$(MAKE) --no-print-directory $(APP)
	@mkdir -p $(PREFIX)/bin
	install -m 0755 bin/$(APP) $(PREFIX)/bin/$(APP)
	@echo "installed $(PREFIX)/bin/$(APP)"

new-app: ## Scaffold a new app: make new-app NAME=foo
	@test -n "$(NAME)" || { echo "usage: make new-app NAME=<name>"; exit 1; }
	./scripts/new-app.sh $(NAME)

dist: ## Cross-compile release archives + checksums into ./dist
	@rm -rf dist && mkdir -p dist
	@set -e; for app in $(APPS); do for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; name=$${app}_$${os}_$${arch}; \
		echo "dist $$name"; mkdir -p dist/$$name; \
		(cd apps/$$app && CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o ../../dist/$$name/$$app .); \
		cp apps/$$app/README.md dist/$$name/ ; \
		tar -C dist -czf dist/$$name.tar.gz $$name && rm -rf dist/$$name; \
	done; done
	@cd dist && (command -v sha256sum >/dev/null && sha256sum *.tar.gz || shasum -a 256 *.tar.gz) > checksums.txt
	@echo "archives in ./dist"

screenshots: ## Regenerate docs/screenshots/*.svg from the apps' render tests
	./scripts/screenshots.sh

clean: ## Remove build output
	rm -rf bin dist

help: ## List targets
	@grep -hE '^[a-zA-Z_$$()-]+:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | expand -t 16
