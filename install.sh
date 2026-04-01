#!/bin/sh
# tc installer — single-command install for any Linux/macOS machine
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Hitesh-K-Murali/terminal-code/main/install.sh | sh
#
# Or with a specific version:
#   curl -fsSL ... | sh -s -- --version v1.0.0
#
# What this does:
#   1. Detects your OS and architecture
#   2. Downloads the latest tc binary from GitHub Releases
#   3. Installs to ~/.local/bin (no root needed)
#   4. Adds to PATH if needed
#   5. Runs `tc setup` for first-time configuration

set -e

REPO="Hitesh-K-Murali/terminal-code"
INSTALL_DIR="${TC_INSTALL_DIR:-$HOME/.local/bin}"
VERSION=""

# Parse arguments
while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="$2"; shift 2 ;;
        --dir)     INSTALL_DIR="$2"; shift 2 ;;
        --help)    usage; exit 0 ;;
        *)         echo "Unknown option: $1"; exit 1 ;;
    esac
done

usage() {
    echo "tc installer"
    echo ""
    echo "Usage: curl -fsSL <url> | sh [-s -- OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --version VERSION   Install a specific version (default: latest)"
    echo "  --dir PATH          Install to a custom directory (default: ~/.local/bin)"
    echo "  --help              Show this help"
}

info()  { echo "  \033[0;34m>\033[0m $*"; }
ok()    { echo "  \033[0;32m✓\033[0m $*"; }
warn()  { echo "  \033[0;33m!\033[0m $*"; }
error() { echo "  \033[0;31m✗\033[0m $*" >&2; }

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       error "Unsupported OS: $(uname -s)"; exit 1 ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             error "Unsupported architecture: $(uname -m)"; exit 1 ;;
    esac
}

# Get latest version from GitHub
get_latest_version() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | \
            grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | \
            grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
    else
        error "Neither curl nor wget found. Cannot download."
        exit 1
    fi
}

# Download file
download() {
    local url="$1" dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" -o "$dest"
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$url" -O "$dest"
    fi
}

# Main install flow
main() {
    echo ""
    echo "  \033[1;35mtc\033[0m — Terminal AI Coding Assistant"
    echo ""

    OS=$(detect_os)
    ARCH=$(detect_arch)
    info "Detected: $OS/$ARCH"

    # Get version
    if [ -z "$VERSION" ]; then
        info "Fetching latest version..."
        VERSION=$(get_latest_version)
        if [ -z "$VERSION" ]; then
            warn "Could not fetch latest release. Falling back to build from source."
            install_from_source
            return
        fi
    fi
    info "Version: $VERSION"

    # Download
    BINARY_NAME="tc-${OS}-${ARCH}"
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$BINARY_NAME"

    info "Downloading $DOWNLOAD_URL..."
    TMPFILE=$(mktemp)
    if download "$DOWNLOAD_URL" "$TMPFILE" 2>/dev/null; then
        ok "Downloaded"
    else
        warn "Pre-built binary not available. Building from source..."
        rm -f "$TMPFILE"
        install_from_source
        return
    fi

    # Install
    mkdir -p "$INSTALL_DIR"
    mv "$TMPFILE" "$INSTALL_DIR/tc"
    chmod +x "$INSTALL_DIR/tc"
    ok "Installed to $INSTALL_DIR/tc"

    ensure_path
    post_install
}

# Build from source if no release binary exists
install_from_source() {
    if ! command -v go >/dev/null 2>&1; then
        error "Go is required to build from source. Install Go: https://go.dev/dl/"
        exit 1
    fi

    info "Building from source..."
    TMPDIR=$(mktemp -d)
    git clone --depth 1 "https://github.com/$REPO.git" "$TMPDIR/tc" 2>/dev/null
    cd "$TMPDIR/tc"
    go build -ldflags="-s -w" -o tc ./cmd/tc/
    mkdir -p "$INSTALL_DIR"
    mv tc "$INSTALL_DIR/tc"
    chmod +x "$INSTALL_DIR/tc"
    cd /
    rm -rf "$TMPDIR"
    ok "Built and installed to $INSTALL_DIR/tc"

    ensure_path
    post_install
}

# Ensure install dir is in PATH
ensure_path() {
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            # Detect shell config file
            SHELL_NAME=$(basename "$SHELL" 2>/dev/null || echo "bash")
            case "$SHELL_NAME" in
                zsh)  RC_FILE="$HOME/.zshrc" ;;
                fish) RC_FILE="$HOME/.config/fish/config.fish" ;;
                *)    RC_FILE="$HOME/.bashrc" ;;
            esac

            if [ -f "$RC_FILE" ]; then
                if ! grep -q "$INSTALL_DIR" "$RC_FILE" 2>/dev/null; then
                    echo "" >> "$RC_FILE"
                    echo "# tc - Terminal AI Coding Assistant" >> "$RC_FILE"
                    echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$RC_FILE"
                    ok "Added $INSTALL_DIR to PATH in $RC_FILE"
                    warn "Run: source $RC_FILE (or restart your terminal)"
                fi
            fi

            export PATH="$INSTALL_DIR:$PATH"
            ;;
    esac
}

# Post-install: first-time setup
post_install() {
    echo ""
    ok "tc is installed!"
    echo ""

    # Check if API key is already set
    if [ -n "$ANTHROPIC_API_KEY" ] || [ -n "$ANTHROPIC_AUTH_TOKEN" ] || [ -n "$OPENAI_API_KEY" ]; then
        ok "API key detected in environment"
        echo ""
        echo "  Run \033[1mtc\033[0m to start."
    else
        echo "  Quick setup — set your API key:"
        echo ""
        echo "    \033[1mexport ANTHROPIC_API_KEY=sk-ant-...\033[0m"
        echo "    \033[1mtc\033[0m"
        echo ""
        echo "  Or configure permanently:"
        echo ""
        echo "    \033[1mtc setup\033[0m"
    fi

    echo ""
    echo "  Documentation: \033[4mhttps://github.com/$REPO\033[0m"
    echo ""
}

main "$@"
