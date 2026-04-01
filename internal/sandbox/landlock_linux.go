package sandbox

import (
	"fmt"
	"log"
	"path/filepath"
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
		// Silent — app startup handles the messaging
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

