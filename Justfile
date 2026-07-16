set shell := ["zsh", "-cu"]

binary := "bin/vault-rename"
version := `git describe --tags --always --dirty 2>/dev/null || echo dev`

default:
    @just --list

build:
    @mkdir -p bin
    @CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/antopolskiy/vault-rename/internal/cli.Version={{version}}" -o "{{binary}}" ./cmd/vault-rename

test:
    @go test ./...

test-race:
    @go test -race ./...

test-e2e:
    @go test -v ./e2e

test-live-vault:
    @if [[ -z "${VAULT_RENAME_LIVE_VAULT:-}" ]]; then \
      echo "VAULT_RENAME_LIVE_VAULT must point to a local vault"; \
      exit 2; \
    fi; \
    go test -v ./internal/obsidian -run TestLiveVaultReadOnly

fixture-audit:
    @go test -v ./internal/fixtures

public-audit: fixture-audit

fuzz:
    @go test ./internal/obsidian -run '^$' -fuzz FuzzParse -fuzztime 10s

bench:
    @go test -run '^$' -bench . -benchmem ./internal/obsidian

coverage:
    @work="$(mktemp -d)"; \
      mkdir -p "$work/raw"; \
      go test -count=1 -covermode=atomic -coverprofile="$work/unit.out" -coverpkg=./... $(go list ./... | grep -v /e2e); \
      VAULT_RENAME_E2E_COVERDIR="$work/raw" go test -count=1 ./e2e; \
      go tool covdata textfmt -i="$work/raw" -o="$work/e2e.out"; \
      head -1 "$work/unit.out" > coverage.out; \
      tail -n +2 "$work/unit.out" >> coverage.out; \
      tail -n +2 "$work/e2e.out" >> coverage.out; \
      go tool cover -func=coverage.out; \
      go tool cover -html=coverage.out -o coverage.html

lint:
    @if command -v golangci-lint >/dev/null; then \
      golangci-lint run; \
    else \
      "$(go env GOPATH)/bin/golangci-lint" run; \
    fi

check: lint test-race build fixture-audit

[positional-arguments]
run *args:
    @just build
    @"{{binary}}" {{args}}

clean:
    @rm -rf bin
