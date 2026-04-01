package sandbox

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Landlock syscall numbers (x86_64)
const (
	sysLandlockCreateRuleset = 444
	sysLandlockAddRule       = 445
	sysLandlockRestrictSelf  = 446
)

// Landlock constants
const (
	landlockCreateRulesetVersion = 1 << 0

	// ABI v1 access rights
	landlockAccessFSExecute    = 1 << 0
	landlockAccessFSWriteFile  = 1 << 1
	landlockAccessFSReadFile   = 1 << 2
	landlockAccessFSReadDir    = 1 << 3
	landlockAccessFSRemoveDir  = 1 << 4
	landlockAccessFSRemoveFile = 1 << 5
	landlockAccessFSMakeChar   = 1 << 6
	landlockAccessFSMakeDir    = 1 << 7
	landlockAccessFSMakeReg    = 1 << 8
	landlockAccessFSMakeSock   = 1 << 9
	landlockAccessFSMakeFifo   = 1 << 10
	landlockAccessFSMakeBlock  = 1 << 11
	landlockAccessFSMakeSym    = 1 << 12

	// Convenience groups
	landlockAccessFSReadAll  = landlockAccessFSReadFile | landlockAccessFSReadDir
	landlockAccessFSWriteAll = landlockAccessFSWriteFile | landlockAccessFSMakeReg |
		landlockAccessFSMakeDir | landlockAccessFSMakeSym
	landlockAccessFSDeleteAll = landlockAccessFSRemoveDir | landlockAccessFSRemoveFile

	landlockRulePathBeneath = 1
)

// landlockRulesetAttr is the struct for landlock_create_ruleset.
type landlockRulesetAttr struct {
	handledAccessFS uint64
}

// landlockPathBeneathAttr is the struct for landlock_add_rule.
type landlockPathBeneathAttr struct {
	allowedAccess uint64
	parentFd      int32
	_             [4]byte // padding
}

// ApplyLandlock applies filesystem restrictions using the Landlock LSM.
// This is a one-way ratchet: once applied, cannot be removed.
// Returns nil if Landlock is unavailable (caller should use app-level fallback).
func ApplyLandlock(plan *EnforcementPlan, caps PlatformCapabilities) error {
	if !caps.LandlockAvailable {
		log.Println("landlock: unavailable, skipping kernel filesystem enforcement")
		return nil
	}

	// Determine which access rights to restrict
	var handledAccess uint64
	handledAccess = landlockAccessFSReadAll | landlockAccessFSWriteAll |
		landlockAccessFSExecute | landlockAccessFSDeleteAll |
		landlockAccessFSMakeChar | landlockAccessFSMakeBlock |
		landlockAccessFSMakeFifo | landlockAccessFSMakeSock

	// Create ruleset
	attr := landlockRulesetAttr{handledAccessFS: handledAccess}
	rulesetFd, _, errno := syscall.Syscall(
		sysLandlockCreateRuleset,
		uintptr(unsafe.Pointer(&attr)),
		unsafe.Sizeof(attr),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("landlock_create_ruleset: %v", errno)
	}
	defer syscall.Close(int(rulesetFd))

	// Add rules for allowed paths. Landlock is allowlist-based:
	// we add rules for paths we WANT to access, everything else is denied.

	// System paths: read-only
	readOnlyPaths := []string{"/usr", "/lib", "/lib64", "/etc", "/proc", "/sys", "/dev", "/tmp"}
	for _, p := range readOnlyPaths {
		if err := addLandlockPath(int(rulesetFd), p, landlockAccessFSReadAll|landlockAccessFSExecute); err != nil {
			log.Printf("landlock: warning: cannot add read-only rule for %s: %v", p, err)
		}
	}

	// Workspace: read+write (but respect deny_write)
	for _, p := range plan.AllowWritePaths {
		access := uint64(landlockAccessFSReadAll | landlockAccessFSWriteAll)
		if plan.AllowDelete {
			access |= landlockAccessFSDeleteAll
		}
		if err := addLandlockPath(int(rulesetFd), p, access); err != nil {
			log.Printf("landlock: warning: cannot add write rule for %s: %v", p, err)
		}
	}

	// Home directory: read by default (specific denies handled by NOT adding those paths)
	if home, err := filepath.Abs(ExpandPath("~")); err == nil {
		if err := addLandlockPath(int(rulesetFd), home, landlockAccessFSReadAll); err != nil {
			log.Printf("landlock: warning: cannot add read rule for home: %v", err)
		}
	}

	// Enforce
	_, _, errno = syscall.Syscall(sysLandlockRestrictSelf, rulesetFd, 0, 0)
	if errno != 0 {
		return fmt.Errorf("landlock_restrict_self: %v", errno)
	}

	log.Println("landlock: filesystem restrictions applied (irreversible)")
	return nil
}

func addLandlockPath(rulesetFd int, path string, access uint64) error {
	const O_PATH = 0x200000 // Not in syscall package on all platforms
	fd, err := syscall.Open(path, O_PATH|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer syscall.Close(fd)

	attr := landlockPathBeneathAttr{
		allowedAccess: access,
		parentFd:      int32(fd),
	}

	_, _, errno := syscall.Syscall6(
		sysLandlockAddRule,
		uintptr(rulesetFd),
		landlockRulePathBeneath,
		uintptr(unsafe.Pointer(&attr)),
		0, 0, 0,
	)
	if errno != 0 {
		return fmt.Errorf("landlock_add_rule for %s: %v", path, errno)
	}
	return nil
}

// PathChecker is the application-level fallback when Landlock is unavailable.
// It checks paths against the enforcement plan before allowing file operations.
type PathChecker struct {
	plan *EnforcementPlan
}

func NewPathChecker(plan *EnforcementPlan) *PathChecker {
	return &PathChecker{plan: plan}
}

// CheckRead returns an error if the path is in deny_read.
func (pc *PathChecker) CheckRead(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	for _, pattern := range pc.plan.DenyReadPaths {
		if matchPath(absPath, pattern) {
			return fmt.Errorf("read denied: %s matches restriction %s [enforcement: %s]",
				path, pattern, pc.plan.FilesystemLevel)
		}
	}
	return nil
}

// CheckWrite returns an error if the path is in deny_write or not in allow_write.
func (pc *PathChecker) CheckWrite(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	// Check deny list first
	for _, pattern := range pc.plan.DenyWritePaths {
		if matchPath(absPath, pattern) {
			return fmt.Errorf("write denied: %s matches restriction %s [enforcement: %s]",
				path, pattern, pc.plan.FilesystemLevel)
		}
	}

	// If allow_write is specified, path must match at least one allow pattern
	if len(pc.plan.AllowWritePaths) > 0 {
		allowed := false
		for _, pattern := range pc.plan.AllowWritePaths {
			if matchPath(absPath, pattern) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("write denied: %s not in allow_write list [enforcement: %s]",
				path, pc.plan.FilesystemLevel)
		}
	}

	return nil
}

// CheckDelete returns an error if deletion is not allowed.
func (pc *PathChecker) CheckDelete(path string) error {
	if !pc.plan.AllowDelete {
		return fmt.Errorf("delete denied: filesystem.allow_delete=false [enforcement: %s]",
			pc.plan.FilesystemLevel)
	}
	return pc.CheckWrite(path) // Must also pass write check
}

// matchPath checks if an absolute path matches a glob pattern.
func matchPath(absPath, pattern string) bool {
	// Handle ** (recursive glob)
	if strings.Contains(pattern, "**") {
		// Split on ** and check if path contains all parts in order
		parts := strings.Split(pattern, "**")
		remaining := absPath
		for _, part := range parts {
			part = strings.Trim(part, "/")
			if part == "" {
				continue
			}
			idx := strings.Index(remaining, part)
			if idx < 0 {
				return false
			}
			remaining = remaining[idx+len(part):]
		}
		return true
	}

	// Standard filepath.Match
	matched, err := filepath.Match(pattern, absPath)
	if err != nil {
		return false
	}
	return matched
}
