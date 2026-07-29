#!/usr/bin/env bash
# Publishes this package to a local Arca instance and installs it back, so an npm
# repository can be checked end to end with the real npm client.
set -euo pipefail

REGISTRY_HOST="${REGISTRY_HOST:-http://localhost:5173}"
REPOSITORY="${REPOSITORY:-npm-hosted}"
EMAIL="${EMAIL:-}"
PASSWORD="${PASSWORD:-}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REGISTRY="$REGISTRY_HOST/repository/$REPOSITORY/"
AUTH_KEY="${REGISTRY#http://}"
AUTH_KEY="${AUTH_KEY#https://}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
fail() { printf '\033[31m%s\033[0m\n' "$1" >&2; exit 1; }

if [ -z "$EMAIL" ] || [ -z "$PASSWORD" ]; then
  fail "Set EMAIL and PASSWORD to an Arca account that can write to '$REPOSITORY'."
fi

step "Checking $REGISTRY is reachable"
curl -fsS "$REGISTRY-/ping" > /dev/null || fail "No registry at $REGISTRY. Is 'make dev' running and does the '$REPOSITORY' repository exist?"

step "Logging in as $EMAIL"
TOKEN="$(curl -fsS -X PUT "$REGISTRY-/user/org.couchdb.user:$EMAIL" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"type\":\"user\"}" \
  | sed -E 's/.*"token":"([^"]+)".*/\1/')"
[ -n "$TOKEN" ] && [ "${TOKEN#cnx_}" != "$TOKEN" ] || fail "Login failed. Check EMAIL and PASSWORD."

# The _authToken key is the registry URL without a scheme, trailing slash kept.
NPMRC="$WORK/npmrc"
cat > "$NPMRC" <<EOF
registry=$REGISTRY
//$AUTH_KEY:_authToken=$TOKEN
EOF

VERSION="1.0.$(date +%s)"
PACKAGE="$(node -p "require('$HERE/package.json').name")"

step "Publishing $PACKAGE@$VERSION"
cp -r "$HERE/package.json" "$HERE/src" "$WORK/"
cp "$NPMRC" "$WORK/.npmrc"
(cd "$WORK" && npm version "$VERSION" --no-git-tag-version > /dev/null && npm publish)

step "Reading it back with npm view"
(cd "$WORK" && npm view "$PACKAGE@$VERSION")

step "Publishing a prerelease under the beta tag"
(cd "$WORK" && npm version "2.0.0-beta.$(date +%s)" --no-git-tag-version > /dev/null && npm publish --tag beta)

step "dist-tags (latest must be the release, not the prerelease)"
(cd "$WORK" && npm dist-tag ls "$PACKAGE")

CONSUMER="$WORK/consumer"
mkdir -p "$CONSUMER"
cp "$NPMRC" "$CONSUMER/.npmrc"
echo '{"name":"consumer","version":"1.0.0","private":true}' > "$CONSUMER/package.json"

step "Installing $PACKAGE@$VERSION into a fresh project"
(cd "$CONSUMER" && npm install "$PACKAGE@$VERSION" --no-audit --no-fund)

step "Running the installed code"
RESULT="$(cd "$CONSUMER" && node --input-type=module -e "
  import { greet } from '$PACKAGE'
  console.log(greet('arca'))
")"
[ "$RESULT" = "hello, arca" ] || fail "Installed package returned '$RESULT'."
echo "$RESULT"

step "Confirming npm resolved the tarball from $REGISTRY_HOST"
grep -q "\"resolved\": \"$REGISTRY" "$CONSUMER/package-lock.json" \
  || fail "The lockfile does not point at the local registry."
grep -o '"integrity": "sha512-[^"]*"' "$CONSUMER/package-lock.json" | head -1

step "Republishing the same version must be refused"
if (cd "$WORK" && npm version "$VERSION" --allow-same-version --no-git-tag-version > /dev/null && npm publish > /dev/null 2>&1); then
  fail "The server accepted a republish of an existing version."
fi
echo "refused, as expected"

printf '\n\033[32mAll npm checks passed against %s\033[0m\n' "$REGISTRY"
