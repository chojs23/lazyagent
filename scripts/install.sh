#!/bin/sh

set -eu

OWNER="chojs23"
REPO="lazyagent"
PROJECT="lazyagent"
VERSION="${VERSION:-latest}"
BIN_DIR="${BIN_DIR:-${HOME}/.local/bin}"

usage() {
  cat <<EOF
Install lazyagent from GitHub release assets.

Usage:
  install.sh [--version <tag>|latest] [--bin-dir <dir>]

Environment:
  VERSION   Release tag to install. Default: latest
  BIN_DIR   Install directory for the lazyagent binary. Default: \$HOME/.local/bin
EOF
}

log() {
  printf '%s\n' "$*" >&2
}

die() {
  log "error: $*"
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --version)
        [ "$#" -ge 2 ] || die "--version requires a value"
        VERSION="$2"
        shift 2
        ;;
      --bin-dir)
        [ "$#" -ge 2 ] || die "--bin-dir requires a value"
        BIN_DIR="$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "unknown argument: $1"
        ;;
    esac
  done
}

detect_platform() {
  case "$(uname -s)" in
    Linux)
      OS="linux"
      ;;
    Darwin)
      OS="darwin"
      ;;
    *)
      die "unsupported operating system: $(uname -s)"
      ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64)
      ARCH="amd64"
      ;;
    arm64|aarch64)
      ARCH="arm64"
      ;;
    *)
      die "unsupported architecture: $(uname -m)"
      ;;
  esac
}

release_api_url() {
  if [ "$VERSION" = "latest" ]; then
    printf 'https://api.github.com/repos/%s/%s/releases/latest' "$OWNER" "$REPO"
    return
  fi
  printf 'https://api.github.com/repos/%s/%s/releases/tags/%s' "$OWNER" "$REPO" "$VERSION"
}

fetch_release_json() {
  curl -fsSL "$(release_api_url)"
}

extract_tag() {
  printf '%s' "$1" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1
}

extract_asset_urls() {
  printf '%s' "$1" |
    tr ',' '\n' |
    sed -n 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p'
}

pick_archive_url() {
  asset_urls="$1"
  archive_url="$(printf '%s\n' "$asset_urls" |
    grep '/'"$PROJECT"'' |
    grep '\.tar\.gz$' |
    grep -i "$OS" |
    grep -E '(amd64|x86_64|arm64|aarch64)' |
    grep -E "$(archive_arch_pattern)" |
    head -n 1 || true)"

  [ -n "$archive_url" ] || die "could not find a release asset for $OS/$ARCH"
  printf '%s' "$archive_url"
}

archive_arch_pattern() {
  case "$ARCH" in
    amd64)
      printf 'amd64|x86_64'
      ;;
    arm64)
      printf 'arm64|aarch64'
      ;;
  esac
}

pick_checksums_url() {
  asset_urls="$1"
  checksums_url="$(printf '%s\n' "$asset_urls" | grep '/checksums\.txt$' | head -n 1 || true)"
  [ -n "$checksums_url" ] || die "could not find checksums.txt in the release assets"
  printf '%s' "$checksums_url"
}

download_file() {
  url="$1"
  output="$2"
  curl -fsSL "$url" -o "$output"
}

verify_checksum() {
  archive_path="$1"
  checksums_path="$2"
  archive_name="$(basename "$archive_path")"
  expected_line="$(grep "  $archive_name\$" "$checksums_path" || true)"
  [ -n "$expected_line" ] || die "missing checksum for $archive_name"
  expected_sum="$(printf '%s' "$expected_line" | awk '{print $1}')"
  actual_sum="$(sha256_file "$archive_path")"
  [ "$expected_sum" = "$actual_sum" ] || die "checksum mismatch for $archive_name"
}

sha256_file() {
  file_path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file_path" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file_path" | awk '{print $1}'
    return
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$file_path" | awk '{print $NF}'
    return
  fi
  die "missing checksum tool: need one of sha256sum, shasum, or openssl"
}

extract_archive() {
  archive_path="$1"
  output_dir="$2"
  tar -xzf "$archive_path" -C "$output_dir"
}

install_binary() {
  source_dir="$1"
  mkdir -p "$BIN_DIR"
  binary_path="$source_dir/$PROJECT"
  [ -f "$binary_path" ] || die "expected extracted binary at $binary_path"
  install -m 0755 "$binary_path" "$BIN_DIR/$PROJECT"
}

print_success() {
  log "installed $PROJECT to $BIN_DIR/$PROJECT"
  case ":$PATH:" in
    *":$BIN_DIR:"*)
      ;;
    *)
      log "note: $BIN_DIR is not on PATH"
      ;;
  esac
}

main() {
  parse_args "$@"
  require_cmd curl
  require_cmd tar
  require_cmd install
  require_cmd awk
  require_cmd grep
  require_cmd sed

  detect_platform

  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"' EXIT INT TERM HUP

  release_json="$(fetch_release_json)"
  resolved_tag="$(extract_tag "$release_json")"
  [ -n "$resolved_tag" ] || die "could not resolve release tag"

  asset_urls="$(extract_asset_urls "$release_json")"
  [ -n "$asset_urls" ] || die "could not read release assets"

  archive_url="$(pick_archive_url "$asset_urls")"
  checksums_url="$(pick_checksums_url "$asset_urls")"

  archive_path="$tmp_dir/$(basename "$archive_url")"
  checksums_path="$tmp_dir/checksums.txt"
  extract_dir="$tmp_dir/extract"
  mkdir -p "$extract_dir"

  log "installing $PROJECT $resolved_tag for $OS/$ARCH"
  download_file "$archive_url" "$archive_path"
  download_file "$checksums_url" "$checksums_path"
  verify_checksum "$archive_path" "$checksums_path"
  extract_archive "$archive_path" "$extract_dir"
  install_binary "$extract_dir"
  print_success
}

main "$@"
