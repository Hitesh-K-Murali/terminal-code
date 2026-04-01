//go:build !linux

package sandbox

import "log"

// ApplyProcessSecurity is a no-op on non-Linux platforms.
func ApplyProcessSecurity(caps PlatformCapabilities) error {
	log.Println("sandbox: kernel security features require Linux — skipping")
	return nil
}
