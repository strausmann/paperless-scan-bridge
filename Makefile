# Phase 1 Makefile stub.
#
# Targets mirror the suite documented in CONTRIBUTING.md so
# contributors can already discover the intended developer
# workflow. Each recipe is a placeholder that prints "TODO Phase 1"
# — the real implementations land alongside the components, lint
# configs, and Ansible roles they exercise.
#
# Conventions:
#   * Recipe lines must be tab-indented (Make requirement).
#   * Targets are PHONY by default; we have no file outputs yet.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c

.DEFAULT_GOAL := help

# ---------------------------------------------------------------------------
# Aggregate targets
# ---------------------------------------------------------------------------

.PHONY: test
test: test-go test-shell test-yaml test-docker test-docs ## Run the full lint+test suite (Phase 1)

.PHONY: lint
lint: ## Run every linter (alias for the lint half of `test`)
	@echo "TODO Phase 1: lint"

# ---------------------------------------------------------------------------
# Per-language test targets
# ---------------------------------------------------------------------------

.PHONY: test-go
test-go: ## Run `go test ./...` across the workspace
	@echo "TODO Phase 1: test-go"

.PHONY: test-shell
test-shell: ## Run shellcheck + bats over deploy/ and backup/scripts/
	@echo "TODO Phase 1: test-shell"

.PHONY: test-yaml
test-yaml: ## Run yamllint over the repository
	@echo "TODO Phase 1: test-yaml"

.PHONY: test-docker
test-docker: ## Run hadolint over every Dockerfile under components/
	@echo "TODO Phase 1: test-docker"

# Fetch the Mermaid and Scalar bundles so the site serves them from
# its own origin instead of letting them load from a CDN at runtime,
# and copy the OpenAPI spec to where Scalar fetches it. All three
# targets are gitignored, so this has to run before any docs build.
.PHONY: docs-vendor
docs-vendor: ## Download pinned Mermaid/Scalar bundles, copy the OpenAPI spec
	./.github/scripts/vendor-mermaid.sh
	./.github/scripts/vendor-scalar.sh
	mkdir -p docs/en/api-reference
	cp components/scan-bridge/api/openapi.yaml docs/en/api-reference/openapi.yaml

# Needs `markdownlint` (npm i -g markdownlint-cli) and `zensical`
# (pip install -r requirements-docs.txt) on PATH. Build order is not
# arbitrary: the English build clears site/, so it must run first or the
# German site under site/de/ is deleted.
.PHONY: test-docs
test-docs: docs-vendor ## markdownlint the docs tree and build both language sites
	markdownlint docs/en docs/de docs/.templates
	zensical build -f zensical.toml --strict
	zensical build -f zensical.de.toml --strict
	@test -f site/index.html || { echo "site/index.html missing"; exit 1; }
	@test -f site/de/index.html || { echo "site/de/index.html missing"; exit 1; }
	python3 .github/scripts/check_no_external_assets.py site

.PHONY: docs-serve
docs-serve: docs-vendor ## Serve the English site locally on :8000
	zensical serve -f zensical.toml

.PHONY: docs-serve-de
docs-serve-de: docs-vendor ## Serve the German site locally on :8001
	zensical serve -f zensical.de.toml --dev-addr localhost:8001

# ---------------------------------------------------------------------------
# Ansible — optional layer under deploy/ansible/
# ---------------------------------------------------------------------------

.PHONY: test-ansible
test-ansible: ## Run ansible-lint over deploy/ansible/
	@echo "TODO Phase 1: test-ansible"

.PHONY: test-molecule
test-molecule: ## Run the full Molecule suite for the optional Ansible roles
	@echo "TODO Phase 1: test-molecule"

# ---------------------------------------------------------------------------
# Self-documenting help target
# ---------------------------------------------------------------------------

.PHONY: help
help: ## Show this help
	@awk 'BEGIN { FS = ":.*?## "; printf "Available targets:\n" } \
	     /^[a-zA-Z0-9_-]+:.*?## / { printf "  %-18s %s\n", $$1, $$2 }' \
	     $(MAKEFILE_LIST)
