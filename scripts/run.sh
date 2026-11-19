#!/usr/bin/env bash
# Запуск приложения. Дополнительные флаги передаются дальше:
#   ./scripts/run.sh -addr 127.0.0.1:9000
set -euo pipefail

# На Windows Git Bash иногда не наследует PATH с Go. Находим go сами.
if ! command -v go >/dev/null 2>&1; then
  for gobin in "/c/Program Files/Go/bin" "/c/Go/bin" "$HOME/go/bin"; do
    if [ -x "$gobin/go.exe" ] || [ -x "$gobin/go" ]; then
      export PATH="$gobin:$PATH"
      break
    fi
  done
fi

cd "$(dirname "$0")/.."
exec go run ./cmd/gotovnost "$@"
