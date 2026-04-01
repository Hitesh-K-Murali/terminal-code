#!/bin/sh
# tc installer — single-command install
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Hitesh-K-Murali/terminal-code/main/install.sh | sh
#
# Options:
#   --version v1.0.0    Install a specific version
#   --dir /custom/path  Install to a custom directory

set -e

REPO="Hitesh-K-Murali/terminal-code"
INSTALL_DIR="${TC_INSTALL_DIR:-$HOME/.local/bin}"
VERSION=""

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="$2"; shift 2 ;;
        --dir)     INSTALL_DIR="$2"; shift 2 ;;
        *)         shift ;;
    esac
done

# Use printf for ANSI — echo doesn't interpret escapes on all shells
info()  { printf "  \033[34m>\033[0m %s\n" "$*"; }
ok()    { printf "  \033[32m✓\033[0m %s\n" "$*"; }
warn()  { printf "  \033[33m!\033[0m %s\n" "$*"; }
fail()  { printf "  \033[31m✗\033[0m %s\n" "$*" >&2; }
bold()  { printf "  \033[1m%s\033[0m\n" "$*"; }

detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       fail "Unsupported OS: $(uname -s)"; exit 1 ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             fail "Unsupported architecture: $(uname -m)"; exit 1 ;;
    esac
}

fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" 2>/dev/null
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$1" 2>/dev/null
    else
        return 1
    fi
}

download() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" -o "$2" 2>/dev/null
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$1" -O "$2" 2>/dev/null
    else
        return 1
    fi
}

get_latest_version() {
    # Try GitHub API
    local response
    response=$(fetch "https://api.github.com/repos/$REPO/releases/latest")
    if [ -n "$response" ]; then
        echo "$response" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/'
        return 0
    fi

    # Fallback: try git ls-remote for tags
    if command -v git >/dev/null 2>&1; then
        git ls-remote --tags "https://github.com/$REPO.git" 2>/dev/null | \
            grep -o 'refs/tags/v[0-9]*\.[0-9]*\.[0-9]*$' | \
            sort -V | tail -1 | sed 's|refs/tags/||'
        return 0
    fi

    return 1
}

main() {
    printf "\n"
    printf "  \033[1;35mtc\033[0m — Terminal AI Coding Assistant\n"
    printf "\n"

    OS=$(detect_os)
    ARCH=$(detect_arch)
    info "Platform: ${OS}/${ARCH}"

    # Get version
    if [ -z "$VERSION" ]; then
        info "Checking latest release..."
        VERSION=$(get_latest_version)
    fi

    if [ -z "$VERSION" ]; then
        warn "Could not determine latest release."
        warn "This usually means GitHub API rate limit (60 req/hr unauthenticated)."
        printf "\n"
        bold "Try one of these instead:"
        printf "\n"
        printf "    # Install a specific version:\n"
        printf "    curl -fsSL ... | sh -s -- --version v0.1.0\n"
        printf "\n"
        printf "    # Build from source (requires Go):\n"
        printf "    git clone https://github.com/%s.git\n" "$REPO"
        printf "    cd terminal-code && make install\n"
        printf "\n"
        exit 1
    fi

    ok "Version: $VERSION"

    # Download binary
    BINARY_NAME="tc-${OS}-${ARCH}"
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/$BINARY_NAME"

    info "Downloading ${BINARY_NAME}..."
    TMPFILE=$(mktemp)
    if download "$DOWNLOAD_URL" "$TMPFILE"; then
        ok "Downloaded"
    else
        fail "Download failed: $DOWNLOAD_URL"
        fail "Check: https://github.com/$REPO/releases/tag/$VERSION"
        rm -f "$TMPFILE"
        exit 1
    fi

    # Verify checksum (if available)
    CHECKSUM_URL="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"
    CHECKSUM_FILE=$(mktemp)
    if download "$CHECKSUM_URL" "$CHECKSUM_FILE" 2>/dev/null; then
        if command -v sha256sum >/dev/null 2>&1; then
            EXPECTED=$(grep "$BINARY_NAME" "$CHECKSUM_FILE" | awk '{print $1}')
            ACTUAL=$(sha256sum "$TMPFILE" | awk '{print $1}')
        elif command -v shasum >/dev/null 2>&1; then
            EXPECTED=$(grep "$BINARY_NAME" "$CHECKSUM_FILE" | awk '{print $1}')
            ACTUAL=$(shasum -a 256 "$TMPFILE" | awk '{print $1}')
        fi

        if [ -n "$EXPECTED" ] && [ -n "$ACTUAL" ]; then
            if [ "$EXPECTED" = "$ACTUAL" ]; then
                ok "Checksum verified"
            else
                fail "Checksum mismatch!"
                fail "  Expected: $EXPECTED"
                fail "  Got:      $ACTUAL"
                rm -f "$TMPFILE" "$CHECKSUM_FILE"
                exit 1
            fi
        fi
    fi
    rm -f "$CHECKSUM_FILE"

    # Install
    mkdir -p "$INSTALL_DIR"
    mv "$TMPFILE" "$INSTALL_DIR/tc"
    chmod +x "$INSTALL_DIR/tc"
    ok "Installed to $INSTALL_DIR/tc"

    # Verify binary works
    if "$INSTALL_DIR/tc" version >/dev/null 2>&1; then
        ok "Binary verified: $($INSTALL_DIR/tc version 2>&1)"
    else
        fail "Binary verification failed — the download may be corrupted"
        rm -f "$INSTALL_DIR/tc"
        exit 1
    fi

    # Ensure PATH
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            SHELL_NAME=$(basename "${SHELL:-bash}" 2>/dev/null)
            case "$SHELL_NAME" in
                zsh)  RC_FILE="$HOME/.zshrc" ;;
                fish) RC_FILE="$HOME/.config/fish/config.fish" ;;
                *)    RC_FILE="$HOME/.bashrc" ;;
            esac

            if [ -f "$RC_FILE" ] 2>/dev/null; then
                if ! grep -q "$INSTALL_DIR" "$RC_FILE" 2>/dev/null; then
                    printf '\n# tc\nexport PATH="%s:$PATH"\n' "$INSTALL_DIR" >> "$RC_FILE"
                    ok "Added to PATH in $RC_FILE"
                fi
            fi
            export PATH="$INSTALL_DIR:$PATH"
            ;;
    esac

    # Done
    printf "\n"
    ok "tc is installed!"
    printf "\n"

    if [ -n "$ANTHROPIC_API_KEY" ] || [ -n "$ANTHROPIC_AUTH_TOKEN" ] || [ -n "$OPENAI_API_KEY" ]; then
        ok "API key detected"
        printf "\n"
        bold "Run: tc"
    else
        printf "  Set your API key and start:\n"
        printf "\n"
        printf "    \033[1mexport ANTHROPIC_API_KEY=sk-ant-...\033[0m\n"
        printf "    \033[1mtc\033[0m\n"
        printf "\n"
        printf "  Or run the interactive setup:\n"
        printf "\n"
        printf "    \033[1mtc setup\033[0m\n"
    fi
    printf "\n"
}

main "$@"
