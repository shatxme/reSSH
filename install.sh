#!/usr/bin/env bash
set -euo pipefail

REPO="shatxme/reSSH"
BIN_NAME="ressh"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
TMPDIR_RESSH=""

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'Error: required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf 'linux' ;;
    Darwin) printf 'darwin' ;;
    *)
      printf 'Error: unsupported OS: %s\n' "$(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    *)
      printf 'Error: unsupported architecture: %s\n' "$(uname -m)" >&2
      exit 1
      ;;
  esac
}

download() {
  local url="$1"
  local output="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$output"
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -qO "$output" "$url"
    return
  fi

  printf 'Error: curl or wget is required\n' >&2
  exit 1
}

main() {
  need_cmd tar
  need_cmd mktemp

  local os arch archive url
  os="$(detect_os)"
  arch="$(detect_arch)"
  archive="${BIN_NAME}_${os}_${arch}.tar.gz"
  url="https://github.com/${REPO}/releases/latest/download/${archive}"
  TMPDIR_RESSH="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR_RESSH"' EXIT

  printf 'Installing %s for %s/%s\n' "$BIN_NAME" "$os" "$arch"
  download "$url" "$TMPDIR_RESSH/$archive"

  mkdir -p "$INSTALL_DIR"
  tar -xzf "$TMPDIR_RESSH/$archive" -C "$TMPDIR_RESSH"
  install -m 0755 "$TMPDIR_RESSH/${BIN_NAME}_${os}_${arch}/${BIN_NAME}" "$INSTALL_DIR/$BIN_NAME"

  printf 'Installed to %s\n' "$INSTALL_DIR/$BIN_NAME"
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) printf 'Add %s to PATH if needed.\n' "$INSTALL_DIR" ;;
  esac
  printf 'Run `%s help` to get started.\n' "$BIN_NAME"
}

main "$@"
