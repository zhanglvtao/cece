#!/usr/bin/env bash
# install.sh — one-step setup for cece
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[cece]${NC} $*"; }
warn()  { echo -e "${YELLOW}[cece]${NC} $*"; }
die()   { echo -e "${RED}[cece]${NC} $*" >&2; exit 1; }

# ── Go ──────────────────────────────────────────────────────────────────────
need_go_version="1.24"

if command -v go &>/dev/null; then
    go_ver=$(go version | awk '{print $3}' | sed 's/go//')
    major_minor=$(echo "$go_ver" | cut -d. -f1,2)
    if [ "$(printf '%s\n' "$need_go_version" "$major_minor" | sort -V | head -n1)" = "$need_go_version" ]; then
        info "Go $go_ver found"
    else
        warn "Go $go_ver found, but cece requires >= $need_go_version"
        die "Please upgrade Go: https://go.dev/dl/"
    fi
else
    die "Go not found. Install it first: https://go.dev/dl/"
fi

# ── Build ───────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

info "Downloading dependencies..."
go mod download

info "Building cece..."
go build -o cece ./cmd/cece

info "Build successful: $(pwd)/cece"

# ── Install to PATH (optional) ──────────────────────────────────────────────
if [ -w /usr/local/bin ]; then
    cp cece /usr/local/bin/cece
    info "Installed to /usr/local/bin/cece"
else
    warn "Cannot write to /usr/local/bin (need sudo)."
    warn "You can manually run:  sudo cp cece /usr/local/bin/cece"
    warn "Or add $(pwd) to your PATH."
fi

# ── Config check ────────────────────────────────────────────────────────────
if [ -f ~/.cece/settings.json ]; then
    info "User config found: ~/.cece/settings.json"
else
    warn "No config found at ~/.cece/settings.json"
    warn "Create one with your API key before first use. Example:"
    cat <<'EOF'

  mkdir -p ~/.cece
  cat > ~/.cece/settings.json << 'JSON'
  {
    "provider": {
      "model": "claude-sonnet-4-6",
      "providers": [
        {
          "name": "anthropic",
          "apiKey": "sk-ant-...",
          "baseURL": "https://api.anthropic.com"
        }
      ]
    }
  }
  JSON

EOF
fi

info "Done. Run 'cece' to start."
