package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const restrictionsTemplate = `# tc Restrictions — customize for your project
# These restrictions are compiled into kernel-level enforcement at startup.
# See: tc doctor (to check enforcement levels on your system)

[filesystem]
deny_read = [
    "~/.ssh/**",
    "~/.gnupg/**",
    "~/.aws/**",
    "/etc/shadow",
    "**/.env",
    "**/*.pem",
    "**/*.key",
]

deny_write = [
    "/usr/**",
    "/bin/**",
    "/sbin/**",
    "~/.bashrc",
    "~/.zshrc",
]

allow_write = [
    "./src/**",
    "./internal/**",
    "./configs/**",
    "./tests/**",
    "/tmp/tc-*",
]

allow_delete = false

[commands]
allow = [
    "git", "go", "make", "npm", "npx", "node",
    "python3", "pip", "pytest",
    "cat", "ls", "find", "grep", "rg", "sed", "awk",
    "echo", "printf", "head", "tail", "wc", "sort", "uniq",
    "diff", "patch", "curl",
]

deny_patterns = [
    "* | bash",
    "* | sh",
    "curl * -o *",
    "git push --force *",
    "chmod 777 *",
]

[network]
allow_outbound = [
    "api.anthropic.com:443",
    "api.openai.com:443",
    "localhost:11434",
]
subprocess_network = false

[resources]
max_memory_per_subprocess = "256MB"
max_cpu_cores_per_subprocess = 2
max_subprocess_runtime = "5m"
max_concurrent_subprocesses = 4
max_pids_per_subprocess = 64
max_tokens_per_session = 500000
max_cost_per_session = "5.00"
max_files_written_per_session = 50
max_file_size_write = "1MB"

[llm_actions]
allow_file_read = true
allow_file_write = true
allow_file_delete = false
allow_bash_execution = true
allow_network_fetch = true
allow_git_operations = true
allow_git_push = false
allow_agent_spawn = true
max_parallel_agents = 3
`

// RunInit initializes tc for the current project directory.
func RunInit() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	title := lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true)
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true)
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	fmt.Println(title.Render("  tc init"))
	fmt.Println()

	tcDir := filepath.Join(wd, ".tc")

	// Check if already initialized
	if _, err := os.Stat(tcDir); err == nil {
		fmt.Printf("  %s .tc/ directory already exists\n", muted.Render("i"))
	} else {
		if err := os.MkdirAll(tcDir, 0755); err != nil {
			return fmt.Errorf("create .tc/: %w", err)
		}
		fmt.Printf("  %s Created .tc/\n", ok.Render("✓"))
	}

	// Create memory directory
	memDir := filepath.Join(tcDir, "memory")
	os.MkdirAll(memDir, 0755)

	// Write restrictions template
	restrictPath := filepath.Join(tcDir, "restrictions.toml")
	if _, err := os.Stat(restrictPath); err == nil {
		fmt.Printf("  %s restrictions.toml already exists (not overwritten)\n", muted.Render("i"))
	} else {
		if err := os.WriteFile(restrictPath, []byte(restrictionsTemplate), 0644); err != nil {
			return fmt.Errorf("write restrictions: %w", err)
		}
		fmt.Printf("  %s Created .tc/restrictions.toml\n", ok.Render("✓"))
	}

	// Add .tc/ to .gitignore
	gitignorePath := filepath.Join(wd, ".gitignore")
	if data, err := os.ReadFile(gitignorePath); err == nil {
		if !strings.Contains(string(data), ".tc/") {
			f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0644)
			if err == nil {
				f.WriteString("\n# tc project config\n.tc/\n")
				f.Close()
				fmt.Printf("  %s Added .tc/ to .gitignore\n", ok.Render("✓"))
			}
		} else {
			fmt.Printf("  %s .tc/ already in .gitignore\n", muted.Render("i"))
		}
	}

	fmt.Println()
	fmt.Printf("  Edit %s to customize security restrictions.\n",
		muted.Render(".tc/restrictions.toml"))
	fmt.Printf("  Run %s to verify your setup.\n",
		muted.Render("tc doctor"))
	fmt.Println()

	return nil
}
