package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Restrictions represents the customer-defined security restrictions.
// These are compiled into kernel-level enforcement at startup.
// Once applied, the process itself cannot weaken them.
type Restrictions struct {
	Filesystem  FilesystemRestrictions `toml:"filesystem"`
	Commands    CommandRestrictions    `toml:"commands"`
	Network     NetworkRestrictions    `toml:"network"`
	Resources   ResourceRestrictions   `toml:"resources"`
	LLMActions  LLMActionRestrictions  `toml:"llm_actions"`
	Meta        RestrictionMeta        `toml:"meta"`
}

// FilesystemRestrictions controls which paths the LLM can read/write/delete.
// deny_read/deny_write → Landlock (kernel-enforced on 5.13+, app-level fallback)
// allow_delete=false → Landlock removes unlink permission
type FilesystemRestrictions struct {
	DenyRead    []string `toml:"deny_read"`
	DenyWrite   []string `toml:"deny_write"`
	AllowWrite  []string `toml:"allow_write"`
	AllowDelete bool     `toml:"allow_delete"`
}

// CommandRestrictions controls which binaries subprocesses can execute.
// allow → mount namespace bind-mounts only listed binaries (kernel-enforced)
// deny_patterns → application-level argument filtering (defense-in-depth)
type CommandRestrictions struct {
	Allow        []string `toml:"allow"`
	DenyPatterns []string `toml:"deny_patterns"`
}

// NetworkRestrictions controls outbound network access.
// allow_outbound → Landlock V4 or network namespace (kernel-enforced)
// subprocess_network → whether bash subprocesses get network access at all
type NetworkRestrictions struct {
	AllowOutbound    []string `toml:"allow_outbound"`
	SubprocessNetwork bool    `toml:"subprocess_network"`
}

// ResourceRestrictions controls CPU, memory, and process limits.
// Per-subprocess limits → cgroups v1/v2 (kernel-enforced)
// Per-session limits → application-enforced
type ResourceRestrictions struct {
	MaxMemoryPerSubprocess string `toml:"max_memory_per_subprocess"`
	MaxCPUCoresPerSub      int    `toml:"max_cpu_cores_per_subprocess"`
	MaxSubprocessRuntime   string `toml:"max_subprocess_runtime"`
	MaxConcurrentSubs      int    `toml:"max_concurrent_subprocesses"`
	MaxPidsPerSubprocess   int    `toml:"max_pids_per_subprocess"`

	// Application-enforced
	MaxTokensPerSession     int    `toml:"max_tokens_per_session"`
	MaxCostPerSession       string `toml:"max_cost_per_session"`
	MaxFilesWrittenPerSess  int    `toml:"max_files_written_per_session"`
	MaxFileSizeWrite        string `toml:"max_file_size_write"`
}

// LLMActionRestrictions controls what tool types the LLM can invoke.
type LLMActionRestrictions struct {
	AllowFileRead      bool `toml:"allow_file_read"`
	AllowFileWrite     bool `toml:"allow_file_write"`
	AllowFileDelete    bool `toml:"allow_file_delete"`
	AllowBashExecution bool `toml:"allow_bash_execution"`
	AllowNetworkFetch  bool `toml:"allow_network_fetch"`
	AllowGitOperations bool `toml:"allow_git_operations"`
	AllowGitPush       bool `toml:"allow_git_push"`
	AllowAgentSpawn    bool `toml:"allow_agent_spawn"`
	MaxParallelAgents  int  `toml:"max_parallel_agents"`
}

// RestrictionMeta holds metadata for enterprise-managed configs.
type RestrictionMeta struct {
	ManagedBy         string `toml:"managed_by"`
	PolicyVersion     string `toml:"policy_version"`
	SignatureRequired bool   `toml:"signature_required"`
}

// DefaultRestrictions returns a permissive default that still blocks known-dangerous paths.
func DefaultRestrictions() Restrictions {
	return Restrictions{
		Filesystem: FilesystemRestrictions{
			DenyRead: []string{
				"~/.ssh/**",
				"~/.gnupg/**",
				"~/.aws/**",
				"/etc/shadow",
				"**/.env",
				"**/*.pem",
				"**/*.key",
			},
			DenyWrite: []string{
				"/usr/**",
				"/bin/**",
				"/sbin/**",
				"~/.bashrc",
				"~/.zshrc",
			},
			AllowDelete: false,
		},
		Commands: CommandRestrictions{
			Allow: []string{
				"git", "go", "make", "npm", "npx", "node",
				"python3", "pip", "pytest",
				"cat", "ls", "find", "grep", "rg", "sed", "awk",
				"echo", "printf", "head", "tail", "wc", "sort", "uniq",
				"diff", "patch", "curl",
			},
			DenyPatterns: []string{
				"* | bash",
				"* | sh",
				"curl * -o *",
				"git push --force *",
				"chmod 777 *",
			},
		},
		Network: NetworkRestrictions{
			AllowOutbound: []string{
				"api.anthropic.com:443",
				"api.openai.com:443",
				"localhost:11434",
			},
			SubprocessNetwork: false,
		},
		Resources: ResourceRestrictions{
			MaxMemoryPerSubprocess: "256MB",
			MaxCPUCoresPerSub:      2,
			MaxSubprocessRuntime:   "5m",
			MaxConcurrentSubs:      4,
			MaxPidsPerSubprocess:   64,
			MaxTokensPerSession:    500000,
			MaxCostPerSession:      "5.00",
			MaxFilesWrittenPerSess: 50,
			MaxFileSizeWrite:       "1MB",
		},
		LLMActions: LLMActionRestrictions{
			AllowFileRead:      true,
			AllowFileWrite:     true,
			AllowFileDelete:    false,
			AllowBashExecution: true,
			AllowNetworkFetch:  true,
			AllowGitOperations: true,
			AllowGitPush:       false,
			AllowAgentSpawn:    true,
			MaxParallelAgents:  3,
		},
	}
}

// LoadRestrictions loads restrictions from ~/.tc/restrictions.toml,
// merging with defaults. Returns defaults if file doesn't exist.
func LoadRestrictions() (Restrictions, error) {
	r := DefaultRestrictions()

	home, err := os.UserHomeDir()
	if err != nil {
		return r, nil
	}

	path := filepath.Join(home, ".tc", "restrictions.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil // Use defaults
		}
		return r, fmt.Errorf("read restrictions: %w", err)
	}

	if err := toml.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("parse restrictions %s: %w", path, err)
	}

	return r, nil
}

// ExpandPath resolves ~ and environment variables in restriction paths.
func ExpandPath(pattern string) string {
	if strings.HasPrefix(pattern, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			pattern = filepath.Join(home, pattern[2:])
		}
	}
	return os.ExpandEnv(pattern)
}

// Validate checks for contradictions and impossible configs.
func (r *Restrictions) Validate() []string {
	var warnings []string

	// Check for overlapping allow_write and deny_write
	for _, allow := range r.Filesystem.AllowWrite {
		for _, deny := range r.Filesystem.DenyWrite {
			if pathsOverlap(allow, deny) {
				warnings = append(warnings, fmt.Sprintf(
					"filesystem: allow_write %q overlaps with deny_write %q — deny takes precedence",
					allow, deny))
			}
		}
	}

	// Warn if no commands are allowed but bash execution is enabled
	if r.LLMActions.AllowBashExecution && len(r.Commands.Allow) == 0 {
		warnings = append(warnings,
			"commands.allow is empty but llm_actions.allow_bash_execution is true — subprocesses will have no visible binaries")
	}

	// Warn if delete is allowed in llm_actions but denied in filesystem
	if r.LLMActions.AllowFileDelete && !r.Filesystem.AllowDelete {
		warnings = append(warnings,
			"llm_actions.allow_file_delete=true but filesystem.allow_delete=false — kernel enforcement wins, delete is blocked")
	}

	// Resource sanity checks
	if r.Resources.MaxConcurrentSubs <= 0 {
		r.Resources.MaxConcurrentSubs = 4
		warnings = append(warnings, "resources.max_concurrent_subprocesses must be > 0, defaulting to 4")
	}
	if r.Resources.MaxPidsPerSubprocess <= 0 {
		r.Resources.MaxPidsPerSubprocess = 64
		warnings = append(warnings, "resources.max_pids_per_subprocess must be > 0, defaulting to 64")
	}

	return warnings
}

// pathsOverlap is a basic check for glob pattern overlap.
func pathsOverlap(a, b string) bool {
	// Simple heuristic: if one is a prefix of the other (ignoring globs)
	cleanA := strings.TrimRight(strings.ReplaceAll(a, "**", ""), "/*")
	cleanB := strings.TrimRight(strings.ReplaceAll(b, "**", ""), "/*")
	return strings.HasPrefix(cleanA, cleanB) || strings.HasPrefix(cleanB, cleanA)
}
