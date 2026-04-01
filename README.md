# tc — Terminal AI Coding Assistant with Kernel-Level Security

A terminal-native AI coding assistant built in Go that enforces security restrictions at the **kernel level** — not through application prompts, but through Linux security primitives that the process itself cannot override.

## Why This Exists

Current AI coding assistants (Claude Code, Aider, Cursor) enforce security through **application-level permission prompts** — the user clicks "Allow" and the tool does whatever it wants. If the application has a bug, or the LLM tricks it, those guardrails vanish.

`tc` takes a fundamentally different approach: customer-defined restrictions are compiled into **kernel-level enforcement** using Linux security primitives (Landlock, seccomp, namespaces, cgroups). These are one-way ratchets — once applied at process startup, the process itself cannot weaken them, even with arbitrary code execution.

### The Problem with Application-Level Security

```
Claude Code / Aider / Cursor:
  LLM requests tool → App checks rule → Prompts user Y/N → Executes

  If the app has a bug:    rules bypassed
  If the binary is patched: rules gone
  If the LLM is clever:    regex arms race
```

### How tc Solves It

```
tc:
  LLM requests tool → App checks rule → Kernel enforces restriction → Executes

  If the app has a bug:    kernel still enforces
  If the binary is patched: kernel still enforces
  If the LLM is clever:    binary doesn't exist in subprocess
```

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                         ORCHESTRATOR                              │
│  (Main goroutine — owns terminal, coordinates, renders UI)        │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │                     AGENT POOL                            │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐               │    │
│  │  │ Agent 0   │  │ Agent 1   │  │ Agent 2   │  (bounded)   │    │
│  │  │ goroutine │  │ goroutine │  │ goroutine │              │    │
│  │  │ + budget  │  │ + budget  │  │ + budget  │              │    │
│  │  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘              │    │
│  │        │               │               │                    │    │
│  │  ┌─────▼───────────────▼───────────────▼──────┐           │    │
│  │  │         RESOURCE COORDINATOR                 │           │    │
│  │  │  PathLocker  — per-file RWMutex              │           │    │
│  │  │  GitMutex    — exclusive semaphore           │           │    │
│  │  │  RateLimit   — per-provider token bucket     │           │    │
│  │  │  IOQueue     — serialized terminal output    │           │    │
│  │  │  BudgetMgr   — per-agent token/cost limits   │           │    │
│  │  └──────────────────────────────────────────────┘           │    │
│  └──────────────────────────────────────────────────────────┘    │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │              KERNEL SANDBOX LAYER                         │    │
│  │  seccomp-bpf │ Landlock │ cgroups │ namespaces            │    │
│  └──────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────┘
```

## Security: Four Independent Layers

Each layer is independent. Compromising one doesn't compromise the others.

| Layer | Mechanism | What It Enforces | Can App Override? |
|-------|-----------|-----------------|-------------------|
| **1. Process Isolation** | PID/Mount/Net/User namespaces | Subprocess can't see host processes, only allowed binaries visible, no network | **No** |
| **2. Syscall Filtering** | seccomp-bpf (TSYNC) | Blocks ptrace, process_vm_read/write, kexec across ALL goroutines | **No** |
| **3. Filesystem Sandbox** | Landlock LSM | Kernel denies read/write/delete to restricted paths | **No** |
| **4. Resource Limits** | cgroups v1/v2 | Memory cap, CPU throttle, PID limit per subprocess | **No** |
| **5. Application Policy** | Config-driven rules | Command argument patterns, token budgets, cost limits | Defense-in-depth |

## Customer-Configurable Restrictions

Customers define restrictions in `~/.tc/restrictions.toml`. At startup, these are **compiled into kernel enforcement**:

```toml
[filesystem]
# Kernel-enforced via Landlock — these paths are PHYSICALLY unreadable
deny_read = ["~/.ssh/**", "~/.aws/**", "/etc/shadow", "**/*.key"]
deny_write = ["/usr/**", "/bin/**", "~/.bashrc"]
allow_delete = false  # Landlock removes unlink permission entirely

[commands]
# Mount namespace — unlisted binaries don't exist in subprocess
allow = ["git", "go", "make", "grep", "cat", "ls"]
# rm, wget, nc, nmap → "command not found" (binary invisible)

[network]
# Network namespace — subprocess has no network stack
subprocess_network = false

[resources]
# cgroups — kernel OOM-kills at limit
max_memory_per_subprocess = "256MB"
max_pids_per_subprocess = 64   # Prevents fork bombs
```

### Guarantee Levels

| Restriction | Enforcement | Guarantee |
|---|---|---|
| File read/write/delete deny | Landlock (kernel) | **100%** — kernel denies the syscall |
| Command restriction | Mount namespace (kernel) | **100%** — binary invisible to subprocess |
| Network restriction | Network namespace (kernel) | **100%** — kernel drops packets |
| Memory/CPU/PID limits | cgroups (kernel) | **100%** — kernel OOM-kills/throttles |
| Command argument patterns | Application layer | ~95% — defense-in-depth |
| Token/cost budgets | Application layer | ~95% — application-enforced |

## Parallel Agent Execution

Multiple agents run concurrently with proper isolation:

- **Bounded concurrency** — semaphore-limited agent pool prevents resource exhaustion
- **Per-path file locking** — multiple agents can read the same file; only one can write
- **Git mutex** — `.git/index.lock` is exclusive; agents queue for git operations
- **Serialized output** — single IOQueue prevents terminal output interleaving
- **Per-agent budgets** — each agent has token/cost limits; session budget aggregates

### CPU Model

The workload is **>95% IO-bound** (waiting on LLM API, filesystem, subprocesses). Actual CPU work is JSON marshaling (~microseconds) and terminal rendering (~milliseconds). This means `GOMAXPROCS=4` supports 20+ concurrent agents without CPU contention. The real bottlenecks are API rate limits and file system contention.

## What Makes tc Different

| | Claude Code | Aider | tc |
|---|---|---|---|
| **Language** | TypeScript (512K lines) | Python | Go (~3K lines) |
| **Binary** | Requires Bun runtime | Requires Python | Single static binary |
| **Startup** | ~500ms | ~300ms | ~10ms |
| **Security** | Application-level Y/N prompts | None | Kernel-level enforcement |
| **File restrictions** | App checks path | None | Landlock (kernel denies syscall) |
| **Command restrictions** | Regex pattern matching | None | Mount namespace (binary invisible) |
| **Network restrictions** | None | None | Network namespace (no stack) |
| **Resource limits** | None | None | cgroups (kernel OOM-kill) |
| **Multi-provider** | Anthropic-first | Multi | Multi from day one |
| **Local models** | No | Yes | Yes (Ollama) |
| **Parallel agents** | Sub-agents | No | Bounded pool with coordination |
| **Source leak prevention** | Leaked via npm source maps | Open source | Native binary, garble obfuscation |

## Anti-Leak Architecture

Claude Code's entire source was leaked via npm source maps (March 2026). `tc` eliminates this class:

- **Go compiles to native machine code** — no source maps, no intermediate format
- **garble** obfuscates symbol names and encrypts string constants
- **No secrets in binary** — API keys acquired at runtime via OS keychain
- **Binary integrity check** — self-hash verification at startup
- **Anti-debug** — seccomp blocks ptrace; LD_PRELOAD detection

## Getting Started

```bash
# Build
make build

# Configure API key
export ANTHROPIC_API_KEY=sk-ant-...

# Run
./tc

# Or with config file
mkdir -p ~/.tc
cat > ~/.tc/config.toml << 'EOF'
provider = "anthropic"
model = "claude-sonnet-4-20250514"
EOF

./tc
```

### Customize Security

```bash
# Copy example restrictions
cp configs/restrictions.example.toml ~/.tc/restrictions.toml

# Edit to your needs
vim ~/.tc/restrictions.toml

# tc compiles restrictions to kernel enforcement at startup
./tc
# Output:
#   Platform: kernel 6.5.0
#     seccomp-bpf: available
#     landlock: available (ABI v4)
#     cgroups: v2 (unified)
#   === Enforcement Plan ===
#     [kernel] filesystem: Landlock ABI v4: 7 deny_read, 5 deny_write
#     [kernel] commands: Mount namespace: 14 allowed binaries
#     [kernel] network: Network namespace: subprocess_network=false
#     [kernel] resources: cgroups v2: memory=256MB, cpu=2 cores
```

## Project Structure

```
terminal-code/
├── cmd/tc/main.go              # CLI entrypoint (Cobra)
├── internal/
│   ├── app/                    # Application lifecycle, config
│   ├── ui/                     # Bubbletea TUI (chat, input, theme)
│   ├── engine/                 # LLM interaction loop with tool dispatch
│   ├── provider/               # Multi-provider abstraction (Claude, OpenAI, Ollama)
│   ├── tools/                  # Tool system (read, write, glob, grep, bash)
│   ├── agent/                  # Parallel agent pool, coordinator, budgets
│   ├── sandbox/                # Kernel security (seccomp, Landlock, namespaces, cgroups)
│   │   ├── platform.go         #   Runtime capability detection
│   │   ├── restrictions.go     #   Customer TOML schema
│   │   ├── compiler.go         #   Config → kernel primitives
│   │   ├── seccomp.go          #   Process-wide syscall filter
│   │   ├── landlock.go         #   Filesystem sandbox + app-level fallback
│   │   ├── namespace.go        #   Isolated subprocess spawner
│   │   ├── cgroup.go           #   Resource limits (v1/v2)
│   │   └── audit.go            #   Security event logging
│   ├── policy/                 # Signed policy verification
│   ├── secrets/                # OS keychain, secure memory
│   ├── memory/                 # Persistent project context
│   ├── session/                # Session management, cost tracking
│   ├── plugins/                # WASM plugin runtime
│   └── mcp/                    # Model Context Protocol
├── configs/                    # Default and example configs
├── Makefile
└── go.mod
```

## Requirements

- **Go 1.24+** for building
- **Linux** for kernel security features (seccomp, Landlock, namespaces, cgroups)
  - Kernel 5.13+ for full Landlock filesystem enforcement
  - Kernel 4.18+ for seccomp + namespaces (degraded filesystem enforcement)
- API key for at least one provider (Anthropic, OpenAI, or Ollama for local)

## Roadmap

- [x] Phase 1: Secure foundation (TUI, Claude streaming, seccomp, platform detection)
- [x] Phase 2: Customer-configurable kernel restrictions
- [x] Phase 3: Tool system with sandbox enforcement (read, write, glob, grep, bash)
- [x] Phase 4: Multi-agent parallel execution (agent pool, coordinator, budgets)
- [ ] Phase 5: Multi-provider + intelligent routing (OpenAI, Ollama, auto-routing)
- [ ] Phase 6: Production polish (sessions, git integration, MCP, WASM plugins)

## License

MIT
