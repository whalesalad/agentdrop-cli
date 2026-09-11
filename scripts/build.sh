#!/bin/sh
# Cross-build release archives into dist/.
# Usage: scripts/build.sh [version] [os/arch ...]
# Defaults: version from git describe; the six primary targets.
set -eu
cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
VERSION="${VERSION#v}"
shift $(( $# > 0 ? 1 : 0 ))
TARGETS="${*:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64}"
COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
PKG=github.com/whalesalad/agentdrop-cli/internal/version
LDFLAGS="-s -w -X $PKG.Version=$VERSION -X $PKG.Commit=$COMMIT -X $PKG.Date=$DATE"

rm -rf dist
mkdir -p dist
for target in $TARGETS; do
  os="${target%/*}"; arch="${target#*/}"
  name="agentdrop-$VERSION-$os-$arch"
  stage="dist/$name"
  mkdir -p "$stage"
  bin="agentdrop"; [ "$os" = windows ] && bin="agentdrop.exe"
  echo "building $name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOAMD64=v1 GOARM64=v8.0 \
    go build -trimpath -ldflags "$LDFLAGS" -o "$stage/$bin" ./cmd/agentdrop
  cp LICENSE README.md "$stage/"
  if [ "$os" = windows ]; then
    (cd dist && rm -f "$name.zip" && zip -q -r "$name.zip" "$name")
  else
    tar -C dist -czf "dist/$name.tar.gz" "$name"
  fi
  rm -rf "$stage"
done
(cd dist && sha256sum -- * > checksums.txt)
cat > dist/manifest.json <<JSON
{"version":"$VERSION","commit":"$COMMIT","buildDate":"$DATE","goVersion":"$(go version | awk '{print $3}')","status":"alpha","artifacts":[
$(cd dist && first=1; for f in agentdrop-*; do [ $first = 1 ] || printf ',\n'; first=0; printf '{"name":"%s","sha256":"%s","sizeBytes":%s}' "$f" "$(sha256sum "$f" | cut -d' ' -f1)" "$(stat -c %s "$f")"; done)
]}
JSON
ls -la dist
