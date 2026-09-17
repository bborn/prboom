#!/bin/sh
# prboom installer.
#
#   curl -fsSL https://raw.githubusercontent.com/bborn/prboom/main/install.sh | sh
#
# Downloads a prebuilt binary, so Go is not needed. Everything lands under
# ~/.local/share/prboom, with the commands linked into ~/.local/bin.
#
#   PRBOOM_PREFIX  where the files go      (default ~/.local/share/prboom)
#   PRBOOM_BIN     where the links go      (default ~/.local/bin)
#   PRBOOM_VERSION a tag instead of latest
set -eu

REPO=bborn/prboom
PREFIX=${PRBOOM_PREFIX:-$HOME/.local/share/prboom}
BIN=${PRBOOM_BIN:-$HOME/.local/bin}

say()  { printf '%s\n' "$*"; }
warn() { printf '  ! %s\n' "$*" >&2; }
die()  { printf 'install: %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac
case "$os" in
  darwin|linux) ;;
  *) die "unsupported system: $os" ;;
esac

asset="prboom-$os-$arch.tar.gz"
if [ -n "${PRBOOM_VERSION:-}" ]; then
  url="https://github.com/$REPO/releases/download/$PRBOOM_VERSION/$asset"
else
  url="https://github.com/$REPO/releases/latest/download/$asset"
fi

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar  >/dev/null 2>&1 || die "tar is required"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "downloading $asset"
curl -fsSL "$url" -o "$tmp/$asset" || die "no release asset at $url"
tar -C "$tmp" -xzf "$tmp/$asset"
src=$(find "$tmp" -maxdepth 1 -type d -name 'prboom-*' | head -1)
[ -n "$src" ] || die "the archive did not contain what was expected"

# Replace the payload wholesale so an upgrade cannot leave an old script behind.
rm -rf "$PREFIX"
mkdir -p "$PREFIX" "$BIN"
cp -R "$src"/. "$PREFIX"/
chmod +x "$PREFIX"/bin/* 2>/dev/null || true

for f in "$PREFIX"/bin/*; do
  name=$(basename "$f")
  if [ "$name" = "prboom-env" ]; then continue; fi   # sourced, never run
  ln -sf "$f" "$BIN/$name"
done
say "installed into $PREFIX, linked into $BIN"

# The skill only means anything to Claude Code, and only if it is in a skills
# directory. Link it into every config dir that exists; there is often more
# than one.
linked=0
for d in "$HOME"/.claude "$HOME"/.claude-*; do
  [ -d "$d" ] || continue
  mkdir -p "$d/skills"
  rm -rf "$d/skills/pr-walk"
  ln -s "$PREFIX/skills/pr-walk" "$d/skills/pr-walk"
  linked=$((linked + 1))
done
if [ "$linked" -gt 0 ]; then
  say "linked the pr-walk skill into $linked Claude config dir(s)"
fi

say ""
missing=
for c in git tmux gh jq; do
  command -v "$c" >/dev/null 2>&1 || missing="$missing $c"
done
if [ -n "$missing" ]; then warn "required, and not found:$missing"; fi
command -v delta >/dev/null 2>&1 || warn "optional, for the picker's diff tab and prdiff: delta"
if command -v gh >/dev/null 2>&1; then
  gh auth status >/dev/null 2>&1 || warn "gh is installed but not logged in: gh auth login"
fi

case ":$PATH:" in
  *":$BIN:"*) ;;
  *) warn "$BIN is not on your PATH; add it to your shell profile" ;;
esac

say "done. run prboom inside a git repo."
