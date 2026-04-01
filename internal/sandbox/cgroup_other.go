//go:build !linux

package sandbox

// CgroupManager is a no-op on non-Linux platforms.
type CgroupManager struct{}

func NewCgroupManager(caps PlatformCapabilities, plan *EnforcementPlan) *CgroupManager {
	return &CgroupManager{}
}

func (cm *CgroupManager) Available() bool { return false }

func (cm *CgroupManager) CreateForPID(pid int, label string) (func(), error) {
	return func() {}, nil
}
