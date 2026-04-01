//go:build !linux

package sandbox

// ApplyLandlock is a no-op on non-Linux platforms.
func ApplyLandlock(plan *EnforcementPlan, caps PlatformCapabilities) error {
	return nil
}
