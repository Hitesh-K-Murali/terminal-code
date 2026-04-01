package sandbox

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// EnforcementLevel describes how a restriction is enforced.
type EnforcementLevel int

const (
	EnforcementKernel      EnforcementLevel = iota // Kernel-enforced, cannot be bypassed
	EnforcementApplication                         // Application-enforced, defense-in-depth
	EnforcementDegraded                            // Should be kernel but kernel too old, using app-level
	EnforcementUnavailable                         // Cannot enforce on this platform
)

func (e EnforcementLevel) String() string {
	switch e {
	case EnforcementKernel:
		return "kernel"
	case EnforcementApplication:
		return "application"
	case EnforcementDegraded:
		return "degraded (application fallback)"
	case EnforcementUnavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}

// EnforcementPlan is the compiled output of customer restrictions mapped to
// available kernel primitives. Created once at startup, consumed by all
// sandbox components.
type EnforcementPlan struct {
	// Filesystem enforcement
	FilesystemLevel EnforcementLevel
	DenyReadPaths   []string // Expanded absolute paths
	DenyWritePaths  []string
	AllowWritePaths []string
	AllowDelete     bool

	// Command enforcement via mount namespace
	CommandLevel     EnforcementLevel
	AllowedBinaries  []string // Binary names for mount namespace
	DenyPatterns     []string // Argument-level patterns (app-level)

	// Network enforcement
	NetworkLevel      EnforcementLevel
	AllowedEndpoints  []string // host:port pairs
	SubprocessNetwork bool

	// Resource enforcement via cgroups
	ResourceLevel        EnforcementLevel
	MemoryLimitBytes     int64
	CPUCores             int
	SubprocessTimeout    time.Duration
	MaxConcurrentSubs    int
	MaxPidsPerSubprocess int

	// Application-level session limits
	MaxTokensPerSession    int
	MaxCostPerSession      float64
	MaxFilesWrittenPerSess int
	MaxFileSizeWriteBytes  int64

	// LLM action restrictions (always application-level)
	LLMActions LLMActionRestrictions

	// Audit trail
	Warnings []string
	Applied  []AppliedRestriction
}

// AppliedRestriction records what was applied and how.
type AppliedRestriction struct {
	Category    string
	Description string
	Level       EnforcementLevel
}

// Compile translates customer Restrictions + PlatformCapabilities into
// a concrete EnforcementPlan. This is the "restriction compiler" from the plan.
func Compile(r Restrictions, caps PlatformCapabilities) *EnforcementPlan {
	plan := &EnforcementPlan{
		LLMActions: r.LLMActions,
	}

	compileFilesystem(r, caps, plan)
	compileCommands(r, caps, plan)
	compileNetwork(r, caps, plan)
	compileResources(r, caps, plan)
	compileSessionLimits(r, plan)

	return plan
}

func compileFilesystem(r Restrictions, caps PlatformCapabilities, plan *EnforcementPlan) {
	// Expand all paths
	for _, p := range r.Filesystem.DenyRead {
		plan.DenyReadPaths = append(plan.DenyReadPaths, ExpandPath(p))
	}
	for _, p := range r.Filesystem.DenyWrite {
		plan.DenyWritePaths = append(plan.DenyWritePaths, ExpandPath(p))
	}
	for _, p := range r.Filesystem.AllowWrite {
		plan.AllowWritePaths = append(plan.AllowWritePaths, ExpandPath(p))
	}
	plan.AllowDelete = r.Filesystem.AllowDelete

	if caps.LandlockAvailable {
		plan.FilesystemLevel = EnforcementKernel
		plan.Applied = append(plan.Applied, AppliedRestriction{
			Category:    "filesystem",
			Description: fmt.Sprintf("Landlock ABI v%d: %d deny_read, %d deny_write, delete=%v",
				caps.LandlockABI, len(plan.DenyReadPaths), len(plan.DenyWritePaths), plan.AllowDelete),
			Level: EnforcementKernel,
		})
	} else {
		plan.FilesystemLevel = EnforcementDegraded
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("Landlock unavailable (kernel %s < 5.13). Filesystem restrictions enforced at application level only.",
				caps.KernelVersion))
		plan.Applied = append(plan.Applied, AppliedRestriction{
			Category:    "filesystem",
			Description: fmt.Sprintf("Application-level: %d deny_read, %d deny_write (DEGRADED — upgrade kernel for 100%% enforcement)",
				len(plan.DenyReadPaths), len(plan.DenyWritePaths)),
			Level: EnforcementDegraded,
		})
	}
}

func compileCommands(r Restrictions, caps PlatformCapabilities, plan *EnforcementPlan) {
	plan.AllowedBinaries = r.Commands.Allow
	plan.DenyPatterns = r.Commands.DenyPatterns

	if caps.UserNSAvailable {
		plan.CommandLevel = EnforcementKernel
		plan.Applied = append(plan.Applied, AppliedRestriction{
			Category:    "commands",
			Description: fmt.Sprintf("Mount namespace: %d allowed binaries, all others invisible", len(plan.AllowedBinaries)),
			Level:       EnforcementKernel,
		})
	} else {
		plan.CommandLevel = EnforcementDegraded
		plan.Warnings = append(plan.Warnings,
			"User namespaces unavailable. Command restrictions enforced at application level only.")
		plan.Applied = append(plan.Applied, AppliedRestriction{
			Category:    "commands",
			Description: fmt.Sprintf("Application-level: %d allowed binaries (DEGRADED)", len(plan.AllowedBinaries)),
			Level:       EnforcementDegraded,
		})
	}
}

func compileNetwork(r Restrictions, caps PlatformCapabilities, plan *EnforcementPlan) {
	plan.AllowedEndpoints = r.Network.AllowOutbound
	plan.SubprocessNetwork = r.Network.SubprocessNetwork

	// Network namespace for subprocess isolation works on most kernels
	if caps.UserNSAvailable {
		plan.NetworkLevel = EnforcementKernel
		plan.Applied = append(plan.Applied, AppliedRestriction{
			Category:    "network",
			Description: fmt.Sprintf("Network namespace: subprocess_network=%v, %d allowed endpoints",
				plan.SubprocessNetwork, len(plan.AllowedEndpoints)),
			Level: EnforcementKernel,
		})
	} else {
		plan.NetworkLevel = EnforcementDegraded
		plan.Warnings = append(plan.Warnings,
			"Network namespaces unavailable. Subprocess network restrictions enforced at application level only.")
		plan.Applied = append(plan.Applied, AppliedRestriction{
			Category:    "network",
			Description: "Application-level network restriction (DEGRADED)",
			Level:       EnforcementDegraded,
		})
	}
}

func compileResources(r Restrictions, caps PlatformCapabilities, plan *EnforcementPlan) {
	plan.MemoryLimitBytes = parseByteSize(r.Resources.MaxMemoryPerSubprocess)
	plan.CPUCores = r.Resources.MaxCPUCoresPerSub
	plan.SubprocessTimeout = parseDuration(r.Resources.MaxSubprocessRuntime)
	plan.MaxConcurrentSubs = r.Resources.MaxConcurrentSubs
	plan.MaxPidsPerSubprocess = r.Resources.MaxPidsPerSubprocess

	if plan.MaxConcurrentSubs <= 0 {
		plan.MaxConcurrentSubs = 4
	}

	if caps.CgroupVersion > 0 {
		plan.ResourceLevel = EnforcementKernel
		cgVersion := "v1"
		if caps.CgroupVersion == 2 {
			cgVersion = "v2"
		}
		plan.Applied = append(plan.Applied, AppliedRestriction{
			Category: "resources",
			Description: fmt.Sprintf("cgroups %s: memory=%s, cpu=%d cores, pids=%d, timeout=%s",
				cgVersion, r.Resources.MaxMemoryPerSubprocess, plan.CPUCores,
				plan.MaxPidsPerSubprocess, plan.SubprocessTimeout),
			Level: EnforcementKernel,
		})
	} else {
		plan.ResourceLevel = EnforcementDegraded
		plan.Warnings = append(plan.Warnings,
			"cgroups unavailable. Resource limits enforced via rlimit (partial) and timeouts (application-level).")
		plan.Applied = append(plan.Applied, AppliedRestriction{
			Category:    "resources",
			Description: "rlimit + application-level timeout (DEGRADED)",
			Level:       EnforcementDegraded,
		})
	}
}

func compileSessionLimits(r Restrictions, plan *EnforcementPlan) {
	plan.MaxTokensPerSession = r.Resources.MaxTokensPerSession
	plan.MaxCostPerSession, _ = strconv.ParseFloat(r.Resources.MaxCostPerSession, 64)
	plan.MaxFilesWrittenPerSess = r.Resources.MaxFilesWrittenPerSess
	plan.MaxFileSizeWriteBytes = parseByteSize(r.Resources.MaxFileSizeWrite)

	plan.Applied = append(plan.Applied, AppliedRestriction{
		Category: "session",
		Description: fmt.Sprintf("Application-level: max_tokens=%d, max_cost=$%.2f, max_files=%d, max_file_size=%s",
			plan.MaxTokensPerSession, plan.MaxCostPerSession,
			plan.MaxFilesWrittenPerSess, r.Resources.MaxFileSizeWrite),
		Level: EnforcementApplication,
	})
}

// Report logs the enforcement plan to stderr.
func (p *EnforcementPlan) Report() {
	log.Println("=== Enforcement Plan ===")
	for _, a := range p.Applied {
		log.Printf("  [%s] %s: %s", a.Level, a.Category, a.Description)
	}
	for _, w := range p.Warnings {
		log.Printf("  WARNING: %s", w)
	}
	log.Println("========================")
}

// parseByteSize parses human-readable byte sizes like "256MB", "1GB", "512KB".
func parseByteSize(s string) int64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}

	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "GB"):
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		multiplier = 1024
		s = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}

	val, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return val * multiplier
}

// parseDuration parses duration strings like "5m", "30s", "1h".
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute // default
	}
	return d
}
