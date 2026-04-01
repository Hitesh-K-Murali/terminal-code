package sandbox

import (
	"fmt"
	"log"
	"runtime"

	seccomp "github.com/elastic/go-seccomp-bpf"
)

// ApplyProcessSecurity applies process-wide security restrictions.
// These are one-way ratchets: once applied, the process cannot remove them.
func ApplyProcessSecurity(caps PlatformCapabilities) error {
	if runtime.GOOS != "linux" {
		log.Println("sandbox: non-linux platform, skipping kernel enforcement")
		return nil
	}

	if caps.SeccompAvailable {
		if err := applySeccomp(); err != nil {
			return fmt.Errorf("seccomp: %w", err)
		}
		log.Println("sandbox: seccomp-bpf filter applied (TSYNC)")
	} else {
		log.Println("sandbox: seccomp unavailable, skipping syscall filtering")
	}

	return nil
}

func applySeccomp() error {
	// Block dangerous syscalls. DefaultAction is Allow — we only deny specific calls.
	// TSYNC ensures the filter applies to ALL OS threads (critical for Go's M:N scheduler).
	filter := seccomp.Filter{
		NoNewPrivs: true,
		Flag:       seccomp.FilterFlagTSync,
		Policy: seccomp.Policy{
			DefaultAction: seccomp.ActionAllow,
			Syscalls: []seccomp.SyscallGroup{
				{
					// Block debugging our own process
					Action: seccomp.ActionErrno,
					Names: []string{
						"ptrace",            // No tracing/debugging
						"process_vm_readv",  // No cross-process memory read
						"process_vm_writev", // No cross-process memory write
					},
				},
				{
					// Block kernel-level dangerous operations
					Action: seccomp.ActionErrno,
					Names: []string{
						"kexec_load",      // No kernel replacement
						"kexec_file_load", // No kernel replacement (newer)
						"reboot",          // No reboot
						"swapon",          // No swap manipulation
						"swapoff",         // No swap manipulation
					},
				},
			},
		},
	}

	return seccomp.LoadFilter(filter)
}
