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

.PHONY: test-docs
test-docs: ## Run markdownlint over the documentation tree
	@echo "TODO Phase 1: test-docs"

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
