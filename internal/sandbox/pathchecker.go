package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PathChecker is the application-level filesystem enforcement layer.
// Used on all platforms. On Linux 5.13+, Landlock provides kernel enforcement
// as a stronger layer beneath this. On older kernels and non-Linux, this is
// the only filesystem enforcement.
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

	for _, pattern := range pc.plan.DenyWritePaths {
		if matchPath(absPath, pattern) {
			return fmt.Errorf("write denied: %s matches restriction %s [enforcement: %s]",
				path, pattern, pc.plan.FilesystemLevel)
		}
	}

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
	return pc.CheckWrite(path)
}

// matchPath checks if an absolute path matches a glob pattern.
func matchPath(absPath, pattern string) bool {
	if strings.Contains(pattern, "**") {
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

	matched, err := filepath.Match(pattern, absPath)
	if err != nil {
		return false
	}
	return matched
}
