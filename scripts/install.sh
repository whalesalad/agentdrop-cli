#!/bin/sh
# AgentDrop CLI installer for Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/whalesalad/agentdrop-cli/main/scripts/install.sh | sh
#
# Options (flags or environment):
#   --version vX.Y.Z   AGENTDROP_VERSION    release to install (default: latest)
#   --dir DIR          AGENTDROP_INSTALL_DIR install directory (default: ~/.local/bin)
#   --base-url URL     AGENTDROP_BASE_URL   where release files live (default: GitHub releases)
#   --uninstall                             remove the installed executable
#   --help
#
# What it does: detects OS/arch, downloads the release archive and checksums.txt,
# verifies SHA-256, extracts, and atomically installs `agentdrop` per-user. No
# sudo, no PATH edits (it prints the line to add), credentials never touched.
# Rerunning installs the requested version over the existing one (upgrade or
# rollback). Pipe-friendly: `curl ... | sh -s -- --version v0.3.0`.
set -eu

REPO="whalesalad/agentdrop-cli"
VERSION="${AGENTDROP_VERSION:-}"
INSTALL_DIR="${AGENTDROP_INSTALL_DIR:-$HOME/.local/bin}"
BASE_URL="${AGENTDROP_BASE_URL:-https://github.com/$REPO/releases/download}"
UNINSTALL=0

say() { printf '%s\n' "$*" >&2; }
die() { say "install.sh: $*"; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    --dir) INSTALL_DIR="$2"; shift 2 ;;
    --dir=*) INSTALL_DIR="${1#*=}"; shift ;;
    --base-url) BASE_URL="$2"; shift 2 ;;
    --base-url=*) BASE_URL="${1#*=}"; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) sed -n '2,17p' "$0" 2>/dev/null || say "see script header"; exit 0 ;;
    *) die "unknown option: $1" ;;
  esac
done

BIN="$INSTALL_DIR/agentdrop"

if [ "$UNINSTALL" = 1 ]; then
  if [ -e "$BIN" ]; then rm -f "$BIN"; say "Removed $BIN"; else say "Nothing installed at $BIN"; fi
  say "Saved credentials were left in place. To forget them: run 'agentdrop logout' before uninstalling, or remove ~/.config/agentdrop. Revoke the token in your vault's API tokens page."
  exit 0
fi

# --- tools -------------------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --proto '=https,http' --retry 3 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
else
  die "curl or wget is required"
fi
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
elif command -v openssl >/dev/null 2>&1; then
  sha256() { openssl dgst -sha256 "$1" | sed 's/.*= *//'; }
else
  die "sha256sum, shasum, or openssl is required"
fi
command -v tar >/dev/null 2>&1 || die "tar is required"

# --- target ------------------------------------------------------------------
os=$(uname -s)
case "$os" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) die "unsupported operating system: $os (Linux and macOS are supported; use the Windows installer on Windows)" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) die "unsupported architecture: $arch (amd64 and arm64 are supported)" ;;
esac
# Apple Silicon under Rosetta reports x86_64; install the native build.
if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
  ARCH=arm64
fi

# --- version -----------------------------------------------------------------
TMP=$(mktemp -d 2>/dev/null || mktemp -d -t agentdrop)
trap 'rm -rf "$TMP"' EXIT INT TERM
case "$BASE_URL" in *github.com*) GITHUB=1 ;; *) GITHUB=0 ;; esac

if [ -z "$VERSION" ]; then
  if [ "$GITHUB" = 1 ]; then
    fetch "https://github.com/$REPO/releases/latest/download/manifest.json" "$TMP/manifest.json" || die "could not resolve the latest release"
  else
    fetch "${BASE_URL%/}/manifest.json" "$TMP/manifest.json" || die "could not fetch manifest.json from $BASE_URL"
  fi
  VERSION=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$TMP/manifest.json" | head -n1)
  [ -n "$VERSION" ] || die "manifest.json did not contain a version"
fi
VER="${VERSION#v}"
ASSET="agentdrop-$VER-$OS-$ARCH.tar.gz"
if [ "$GITHUB" = 1 ]; then RELEASE_URL="${BASE_URL%/}/v$VER"; else RELEASE_URL="${BASE_URL%/}"; fi

# --- download and verify -----------------------------------------------------
say "Downloading $ASSET"
fetch "$RELEASE_URL/checksums.txt" "$TMP/checksums.txt" || die "could not download checksums.txt for v$VER"
fetch "$RELEASE_URL/$ASSET" "$TMP/$ASSET" || die "could not download $ASSET (is v$VER a published release with a $OS-$ARCH build?)"
expected=$(grep -E "[[:space:]]\*?$ASSET\$" "$TMP/checksums.txt" | head -n1 | cut -d' ' -f1)
[ -n "$expected" ] || die "checksums.txt does not list $ASSET"
actual=$(sha256 "$TMP/$ASSET")
[ "$expected" = "$actual" ] || die "SHA-256 mismatch for $ASSET (expected $expected, got $actual). Nothing was installed."
say "Checksum verified."

mkdir -p "$TMP/x"
tar -xzf "$TMP/$ASSET" -C "$TMP/x"
src=$(find "$TMP/x" -type f -name agentdrop | head -n1)
[ -n "$src" ] || die "archive did not contain the agentdrop executable"

# --- install -----------------------------------------------------------------
mkdir -p "$INSTALL_DIR"
chmod 0755 "$src"
# Write beside the destination and rename: atomic, and safe while the old binary runs.
staged="$INSTALL_DIR/.agentdrop.$$.tmp"
cp "$src" "$staged"
mv -f "$staged" "$BIN"
installed=$("$BIN" --version 2>/dev/null || true)
[ -n "$installed" ] || die "installed executable did not report a version"
say "Installed agentdrop $installed to $BIN"

# --- PATH advice and conflicts -----------------------------------------------
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    say ""
    say "$INSTALL_DIR is not on your PATH. Add it for your shell, then open a new terminal:"
    case "${SHELL:-}" in
      */zsh) say "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc" ;;
      */fish) say "  fish_add_path $INSTALL_DIR" ;;
      *) say "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.bashrc" ;;
    esac
    ;;
esac
other=$(command -v agentdrop 2>/dev/null || true)
if [ -n "$other" ] && [ "$other" != "$BIN" ]; then
  say "Note: another 'agentdrop' is earlier on PATH: $other. Remove it or reorder PATH if the wrong one runs."
fi
say "Next: agentdrop login"
