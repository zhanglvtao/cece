#!/usr/bin/env bash
# install.sh — one-step install for cece
# Usage: curl -fsSL https://raw.githubusercontent.com/zhanglvtao/cece/main/install.sh | bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[cece]${NC} $*"; }
warn()  { echo -e "${YELLOW}[cece]${NC} $*"; }
die()   { echo -e "${RED}[cece]${NC} $*" >&2; exit 1; }

REPO="zhanglvtao/cece"
INSTALL_DIR="$HOME/.cece/bin"

# ── Detect platform ──────────────────────────────────────────────────────────
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
    darwin|linux) ;;
    *) die "Unsupported OS: $OS (only darwin and linux are supported)" ;;
esac

case "$ARCH" in
    x86_64|amd64) ARCH="x64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) die "Unsupported architecture: $ARCH" ;;
esac

SUFFIX="cece-${OS}-${ARCH}"

# ── Try downloading prebuilt binary from GitHub Releases ─────────────────────
download_binary() {
    info "Fetching latest release version..."
    LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' \
        | head -1 \
        | sed -E 's/.*"v([^"]+)".*/\1/')

    if [ -z "$LATEST" ]; then
        warn "Could not determine latest version from GitHub"
        return 1
    fi

    info "Latest version: v${LATEST}"
    URL="https://github.com/${REPO}/releases/download/v${LATEST}/${SUFFIX}.tar.gz"

    info "Downloading ${SUFFIX}.tar.gz..."
    TMPDIR=$(mktemp -d)
    trap 'rm -rf "$TMPDIR"' EXIT

    if ! curl -fsSL "$URL" -o "$TMPDIR/${SUFFIX}.tar.gz"; then
        warn "Download failed: $URL"
        return 1
    fi

    info "Extracting..."
    mkdir -p "$INSTALL_DIR"
    tar xzf "$TMPDIR/${SUFFIX}.tar.gz" -C "$TMPDIR"

    # tarball 内文件名就是 cece
    if [ -f "$TMPDIR/cece" ]; then
        mv "$TMPDIR/cece" "$INSTALL_DIR/cece"
    elif [ -f "$TMPDIR/$SUFFIX" ]; then
        mv "$TMPDIR/$SUFFIX" "$INSTALL_DIR/cece"
    else
        warn "Unexpected tarball contents"
        return 1
    fi

    chmod +x "$INSTALL_DIR/cece"
    info "Installed to $INSTALL_DIR/cece"
    return 0
}

# ── Fallback: build from source ──────────────────────────────────────────────
build_from_source() {
    need_go_version="1.24"

    if ! command -v go &>/dev/null; then
        die "Go not found and prebuilt binary download failed. Install Go: https://go.dev/dl/"
    fi

    if ! command -v npm &>/dev/null; then
        die "npm not found and source build needs Observatory webapp assets. Install Node.js: https://nodejs.org/"
    fi

    go_ver=$(go version | awk '{print $3}' | sed 's/go//')
    major_minor=$(echo "$go_ver" | cut -d. -f1,2)
    if [ "$(printf '%s\n' "$need_go_version" "$major_minor" | sort -V | head -n1)" != "$need_go_version" ]; then
        die "Go $go_ver found, but cece requires >= $need_go_version"
    fi

    info "Go $go_ver found, building from source..."

    TMPDIR=$(mktemp -d)
    trap 'rm -rf "$TMPDIR"' EXIT

    git clone --depth 1 "https://github.com/${REPO}.git" "$TMPDIR/cece"
    cd "$TMPDIR/cece"

    info "Downloading dependencies..."
    go mod download

    info "Building Observatory webapp..."
    npm --prefix internal/observatory/webapp ci
    npm --prefix internal/observatory/webapp run build

    info "Building cece..."
    go build -ldflags="-s -w" -o cece ./cmd/cece

    mkdir -p "$INSTALL_DIR"
    mv cece "$INSTALL_DIR/cece"
    chmod +x "$INSTALL_DIR/cece"

    info "Built and installed to $INSTALL_DIR/cece"
}

# ── Install ──────────────────────────────────────────────────────────────────
if ! download_binary; then
    warn "Prebuilt binary not available, falling back to build from source"
    build_from_source
fi

# ── PATH setup ───────────────────────────────────────────────────────────────
if ! echo "$PATH" | tr ':' '\n' | grep -q "^${INSTALL_DIR}\$"; then
    echo ""
    warn "$INSTALL_DIR is not in your PATH."
    echo ""
    info "Add it by running:"
    echo ""
    echo "    echo 'export PATH=\"\$HOME/.cece/bin:\$PATH\"' >> ~/.zshrc"
    echo "    source ~/.zshrc"
    echo ""
    info "(Replace ~/.zshrc with ~/.bashrc if you use bash)"
fi

# ── Config check ─────────────────────────────────────────────────────────────
if [ -f ~/.cece/settings.json ]; then
    info "Config found: ~/.cece/settings.json"
else
    echo ""
    warn "No config found at ~/.cece/settings.json"
    info "Downloading example config..."
    mkdir -p ~/.cece
    curl -fsSL "https://raw.githubusercontent.com/${REPO}/main/docs/settings.example.json" \
        -o ~/.cece/settings.json
    info "Config written to ~/.cece/settings.json"
    warn "Edit apiKey in ~/.cece/settings.json before first use"
fi

echo ""
info "Done. Run 'cece' to start."
