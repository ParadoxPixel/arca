#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="$SCRIPT_DIR/build"

mkdir -p "$BUILD_DIR"

echo "Building frontend..."
cd "$SCRIPT_DIR/internal/frontend"
pnpm install --frozen-lockfile
pnpm build

echo "Building repo-server..."
cd "$SCRIPT_DIR"
CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" \
  go build -o "$BUILD_DIR/repo-server" ./cmd/arca

echo "Running Ansible playbook..."
ansible-playbook "$SCRIPT_DIR/deploy/playbook.yml" \
  -i "${INVENTORY:-$SCRIPT_DIR/deploy/inventory}" \
  -e "binary_path=$BUILD_DIR/repo-server" \
  "$@"
