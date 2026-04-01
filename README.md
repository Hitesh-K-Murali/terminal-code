# tc — Terminal AI Coding Assistant

An AI coding assistant that lives in your terminal. One command to install, zero config to start.

```bash
curl -fsSL https://raw.githubusercontent.com/Hitesh-K-Murali/terminal-code/main/install.sh | sh
```

Then run `tc`. That's it.

---

## Quick Start

### Install

```bash
# Option 1: One-line install (recommended)
curl -fsSL https://raw.githubusercontent.com/Hitesh-K-Murali/terminal-code/main/install.sh | sh

# Option 2: Go install
go install github.com/Hitesh-K-Murali/terminal-code/cmd/tc@latest

# Option 3: Build from source
git clone https://github.com/Hitesh-K-Murali/terminal-code.git
cd terminal-code
make install
```

### First Run

```bash
tc
```

On first run, a setup wizard guides you through:
1. **Pick a provider** — Anthropic (Claude), OpenAI (GPT), or Ollama (local, free)
2. **Enter your API key** — masked input, stored with 0600 permissions
3. **Choose a model** — sensible defaults pre-selected

No config files to edit. No environment variables to set. Just run `tc`.

### Already Have an API Key?

If `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or `ANTHROPIC_AUTH_TOKEN` is in your environment, tc uses it automatically — no setup needed.

```bash
export ANTHROPIC_API_KEY=sk-ant-...
tc
```

---

## Commands

| Command | What It Does |
|---|---|
| `tc` | Start the AI assistant (chat TUI) |
| `tc setup` | Re-run the configuration wizard |
| `tc config` | View current configuration |
| `tc config set model gpt-4o` | Change a setting |
| `tc init` | Initialize tc for the current project (restrictions, memory) |
| `tc doctor` | Diagnose issues (config, API, kernel features) |
| `tc upgrade` | Self-update to the latest release |
| `tc version` | Print version |
| `tc completion bash\|zsh\|fish` | Generate shell completions |

### Inside the TUI

| Command | What It Does |
|---|---|
| `/help` | Show available slash commands |
| `/model [name]` | Show or switch the active model |
| `/cost` | Show token usage and estimated cost |
| `/tools` | List available tools |
| `/clear` | Clear conversation history |
| `/quit` | Exit |

**Keyboard:** Enter = send, Ctrl+D = newline, Ctrl+C = quit.

---

## Providers

tc works with any of these — switch anytime with `tc config set provider <name>`:

| Provider | Models | Requires |
|---|---|---|
| **Anthropic** | claude-sonnet-4, claude-opus-4, claude-haiku-4 | API key |
| **OpenAI** | gpt-4o, gpt-4o-mini, o1, o3-mini | API key |
| **Ollama** | llama3.2, codellama, deepseek-coder, mistral, ... | Ollama running locally |

Ollama needs no API key — runs entirely on your machine. Install Ollama, pull a model, run tc.

### Custom API Endpoint

For corporate proxies or self-hosted APIs:

```bash
tc config set base_url https://your-proxy.corp.com
```

Or via environment: `ANTHROPIC_BASE_URL=https://...`

---

## Tools

The AI assistant has 7 built-in tools, each sandboxed:

| Tool | Permission | What It Does |
|---|---|---|
| `read_file` | Read | Read files with line numbers |
| `write_file` | Write | Write/create files (size-limited) |
| `glob` | Read | Find files by pattern (`**/*.go`) |
| `grep` | Read | Search file contents by regex |
| `bash` | Execute | Run shell commands (sandboxed subprocess) |
| `git` | Execute | Git operations (push/rebase/reset blocked) |
| `dir_context` | Read | Get a directory summary (cached) |

Every tool call goes through two layers of enforcement:
1. **Application policy** — configurable allow/deny rules
2. **Kernel enforcement** — Landlock, namespaces, cgroups (cannot be bypassed)

---

## Security

### What Makes tc Different

Other AI coding tools enforce security by asking "Allow? Y/N." If the app has a bug, those rules vanish.

tc compiles your restrictions into **kernel-level enforcement** — Linux security primitives the process itself cannot override, even with arbitrary code execution.

| Layer | Mechanism | What It Enforces | Can App Bypass? |
|---|---|---|---|
| Subprocess isolation | PID/Mount/Net namespaces | Process can't see host, only allowed binaries visible | No |
| Syscall filtering | seccomp-bpf (TSYNC) | Blocks ptrace, process_vm_read/write across all threads | No |
| Filesystem sandbox | Landlock LSM | Kernel denies read/write to restricted paths | No |
| Resource limits | cgroups v1/v2 | Memory cap, CPU throttle, PID limit per subprocess | No |
| Application policy | Config-driven rules | Command patterns, token budgets, cost limits | Defense-in-depth |

### Configuring Restrictions

```bash
tc init   # Creates .tc/restrictions.toml in your project
```

Then edit `.tc/restrictions.toml`:

```toml
[filesystem]
# Kernel-enforced — these paths are physically unreadable by tc
deny_read = ["~/.ssh/**", "~/.aws/**", "/etc/shadow"]
allow_delete = false

[commands]
# Mount namespace — unlisted binaries don't exist in subprocesses
allow = ["git", "go", "make", "grep", "cat", "ls"]

[network]
# Network namespace — subprocesses have no network stack
subprocess_network = false

[resources]
# cgroups — kernel OOM-kills at limit
max_memory_per_subprocess = "256MB"
max_pids_per_subprocess = 64
```

Run `tc doctor` to see what's enforced at kernel level vs application level on your system.

### Kernel Compatibility

| Kernel | Enforcement |
|---|---|
| 5.13+ | Full: Landlock + seccomp + namespaces + cgroups |
| 4.18+ | Partial: seccomp + namespaces + cgroups (filesystem is app-level) |

tc detects capabilities at startup and degrades gracefully with clear warnings.

---

## Multi-Agent Execution

Multiple agents run in parallel with shared resource coordination:

- **Bounded pool** — configurable max concurrent agents (default 3)
- **File locking** — per-path RWMutex (concurrent reads, exclusive writes)
- **Git mutex** — only one git operation at a time
- **Budget limits** — per-agent token/cost caps
- **Message bus** — agents share findings and delegate subtasks
- **Autonomous delegation** — agents spawn sub-agents (sync or async)

---

## Configuration Reference

### Config File (`~/.tc/config.toml`)

```toml
provider = "anthropic"          # anthropic, openai, ollama
api_key = "sk-ant-..."          # API key (not needed for ollama)
model = "claude-sonnet-4-20250514"  # Default model
base_url = ""                   # Custom API endpoint (optional)
ollama_url = ""                 # Custom Ollama URL (default: localhost:11434)
```

### Environment Variables

| Variable | Overrides |
|---|---|
| `ANTHROPIC_API_KEY` | `api_key` (sets provider to anthropic) |
| `ANTHROPIC_AUTH_TOKEN` | `api_key` (alternative auth) |
| `ANTHROPIC_BASE_URL` | `base_url` |
| `OPENAI_API_KEY` | `api_key` (sets provider to openai) |
| `TC_PROVIDER` | `provider` |
| `TC_MODEL` | `model` |

Environment variables always take precedence over the config file.

### Restrictions File (`.tc/restrictions.toml`)

See `tc init` to generate a template with full documentation, or view [the example](configs/restrictions.example.toml).

---

## Upgrade

```bash
tc upgrade
```

Or re-run the install script:

```bash
curl -fsSL https://raw.githubusercontent.com/Hitesh-K-Murali/terminal-code/main/install.sh | sh
```

---

## Troubleshooting

```bash
tc doctor
```

This checks:
- Config file exists and is valid
- API key works (with latency)
- Kernel features available
- File permissions secure
- Network connectivity
- Project restrictions valid

### Common Issues

**"no API key found"** → Run `tc setup` or set `ANTHROPIC_API_KEY` in your environment.

**"Landlock unavailable"** → Your kernel is older than 5.13. Filesystem restrictions work at application level instead. Not a blocker — just reduced enforcement.

**"Cannot reach API"** → Check your network/proxy. For corporate proxies: `tc config set base_url https://your-proxy.com`

**"Ollama not reachable"** → Start Ollama: `ollama serve`. Then: `tc config set provider ollama`

---

## Building from Source

```bash
git clone https://github.com/Hitesh-K-Murali/terminal-code.git
cd terminal-code

make build          # Development binary
make test           # Run tests
make install        # Install to ~/.local/bin
make release        # Cross-compile for all platforms
make build-release  # Obfuscated build (requires garble)
```

Requirements: Go 1.24+, Linux (for kernel security features).

---

## License

MIT
