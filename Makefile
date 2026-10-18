# Developer entry points. Every target wraps an existing npm script or Go
# command so CI and `make` stay in sync. Run `make help` for the list.
#
# Pass extra arguments to the ao CLI with ARGS, e.g. `make ao ARGS="status"`.

SHELL := /bin/bash
.DEFAULT_GOAL := help

ARGS ?=
NPM_DIRS := . frontend packages/cloud-client packages/product-ui packages/hub-bff

DAEMON_CHANNEL ?= main
DAEMON_UPDATE_CMD ?= apply
# Default to the repository this checkout came from.
DAEMON_REPO ?= $(shell git remote get-url origin 2>/dev/null | sed -E 's|^.*github\.com[:/]||; s|\.git$$||')

.PHONY: help setup doctor \
	dev dev-cloud dev-web daemon ao \
	build build-backend build-daemon daemon-dist daemon-channel package dist \
	daemon-update-install daemon-update \
	test test-backend test-race test-frontend test-e2e test-e2e-renderer test-daemon-update \
	lint vet typecheck check \
	sqlc api api-spec api-ts \
	cloud-check product-ui-check hub-bff-check shared-check \
	cloud-local cloud-local-down cloud-local-reset cloud-local-smoke \
	ci-local

help: ## Show this help
	@echo "Usage: make <target> [ARGS=...]"
	@echo
	@grep -hE '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

##@ Setup

setup: ## Check prerequisites, install all npm and Go dependencies, then run doctor
	@bash scripts/doctor.sh --prereqs
	@for dir in $(NPM_DIRS); do \
		echo "==> npm ci ($$dir)"; \
		npm ci --prefix $$dir || exit 1; \
	done
	@echo "==> go mod download (backend)"
	@cd backend && go mod download
	@bash scripts/doctor.sh

doctor: ## Check toolchain versions and installed dependencies
	@bash scripts/doctor.sh

##@ Run

dev: ## Run the Electron desktop app in dev mode (builds the daemon first)
	VITE_AO_DEVELOPER_MODE_DEFAULT=on npm --prefix frontend run dev

dev-cloud: ## Run the desktop app against the local cloud stack (start it with make cloud-local)
	VITE_AO_DEVELOPER_MODE_DEFAULT=on \
	AO_CLOUD_OFFERING=on \
	AO_CLOUD_CONTROL_PLANE_URL=http://127.0.0.1:$${AO_CLOUD_PORT:-8081} \
	npm --prefix frontend run dev

dev-web: ## Run the renderer only in a browser (no Electron)
	npm --prefix frontend run dev:web

daemon: ## Run the loopback daemon from source
	cd backend && go run ./cmd/ao daemon

ao: ## Run the ao CLI from source, e.g. make ao ARGS="status"
	cd backend && go run ./cmd/ao $(ARGS)

##@ Build

build-backend: ## Build all backend Go packages
	cd backend && go build ./...

build-daemon: ## Build the daemon binary bundled into the desktop app (dev)
	npm --prefix frontend run build:daemon -- --dev

daemon-dist: ## Portable ao daemon + Node 24 LTS for macOS/Linux (arm64/amd64) in dist/daemon; DAEMON_PLATFORMS="linux/amd64", DAEMON_NODE=0
	@bash scripts/build-daemon-dist.sh

daemon-channel: ## Stage the daemon update channel in dist/daemon-channel (bundles, SHA256SUMS, manifest.json)
	@bash scripts/build-daemon-channel.sh

build: build-backend ## Build backend and package the desktop app
	npm --prefix frontend run package

package: ## Package the desktop app for the current platform
	npm --prefix frontend run package

dist: ## Create desktop distributables (needs platform packaging tools)
	npm --prefix frontend run make

##@ Self-hosted host

daemon-update-install: ## Install or refresh this host's self-update timer; DAEMON_CHANNEL=main, ARGS="--dry-run"
	@bash scripts/install-self-update.sh --install --channel $(DAEMON_CHANNEL) --repo $(DAEMON_REPO) $(ARGS)

daemon-update: ## Run this host's installed updater; DAEMON_UPDATE_CMD=apply|check|status|rollback
	@$$HOME/.ao/bin/ao-self-update $(DAEMON_UPDATE_CMD) $(ARGS)

##@ Test and lint

test: test-backend test-frontend ## Run backend and frontend unit tests

test-backend: ## Run backend Go tests
	cd backend && go test ./...

test-race: ## Run backend Go tests with the race detector
	cd backend && go test -race ./...

test-frontend: ## Run frontend Vitest unit tests
	npm --prefix frontend run test

test-e2e: ## Run frontend Playwright e2e tests
	npm --prefix frontend run test:e2e

test-e2e-renderer: ## Run the T0/P0 renderer Playwright subset
	npm --prefix frontend run test:e2e:renderer

test-daemon-update: ## Run the daemon self-update script tests
	@bash scripts/ao-self-update.test.sh all
	@bash scripts/install-self-update.test.sh all

lint: ## Backend go test + golangci-lint (pinned)
	npm run lint

vet: ## Run go vet on the backend
	cd backend && go vet ./...

typecheck: ## Typecheck the frontend
	npm run frontend:typecheck

check: lint vet typecheck test-frontend ## Run the main local checks before a PR

##@ Code generation

sqlc: ## Regenerate sqlc storage code
	npm run sqlc

api: ## Regenerate OpenAPI spec and frontend TS types
	npm run api

api-spec: ## Regenerate the OpenAPI spec only
	npm run api:spec

api-ts: ## Regenerate frontend TS types from the spec only
	npm run api:ts

##@ Shared packages

cloud-check: ## Generate, typecheck, test, and build the cloud client
	npm run cloud:check

product-ui-check: ## Typecheck, test, and build product-ui
	npm run product-ui:check

hub-bff-check: ## Typecheck and test the hub web BFF
	npm run hub-bff:check

shared-check: ## Run cloud-check and product-ui-check
	npm run shared:check

##@ Local cloud stack

cloud-local: ## Start the local cloud stack
	npm run cloud:local

cloud-local-down: ## Stop the local cloud stack
	npm run cloud:local:down

cloud-local-reset: ## Reset the local cloud stack
	npm run cloud:local:reset

cloud-local-smoke: ## Smoke-test the local cloud stack
	npm run cloud:local:smoke

##@ CI

ci-local: ## Run all GitHub workflows locally via agent-ci (needs Docker)
	npx @redwoodjs/agent-ci run --all
