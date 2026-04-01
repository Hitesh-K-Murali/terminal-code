//go:build !linux

package sandbox

// DetectPlatform returns empty capabilities on non-Linux platforms.
// Kernel security features (seccomp, Landlock, namespaces, cgroups) are Linux-specific.
func DetectPlatform() PlatformCapabilities {
	return PlatformCapabilities{}
}
