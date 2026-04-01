package sandbox

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// DetectPlatform probes the running kernel for available security features.
// This is called once at startup; the result is passed to all sandbox components.
func DetectPlatform() PlatformCapabilities {
	caps := PlatformCapabilities{}

	// Kernel version
	var uname syscall.Utsname
	if err := syscall.Uname(&uname); err == nil {
		caps.KernelVersion = utsToString(uname.Release)
		caps.KernelMajor, caps.KernelMinor = parseKernelVersion(caps.KernelVersion)
	}

	// seccomp: available if kernel has CONFIG_SECCOMP_FILTER
	// We check by reading /proc/sys/kernel/seccomp/actions_avail or
	// by checking if prctl(PR_GET_SECCOMP) doesn't return EINVAL
	caps.SeccompAvailable = detectSeccomp()

	// Landlock: available on Linux 5.13+
	// Check by probing the ABI version via landlock_create_ruleset(NULL, 0, LANDLOCK_CREATE_RULESET_VERSION)
	caps.LandlockAvailable, caps.LandlockABI = detectLandlock()

	// cgroups: v1 or v2
	caps.CgroupVersion = detectCgroupVersion()

	// User namespaces
	caps.UserNSAvailable = detectUserNS()

	return caps
}

func utsToString(arr [65]int8) string {
	var buf []byte
	for _, b := range arr {
		if b == 0 {
			break
		}
		buf = append(buf, byte(b))
	}
	return string(buf)
}

func parseKernelVersion(version string) (major, minor int) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) >= 1 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		// Minor might have a suffix like "18-something"
		minorStr := parts[1]
		if idx := strings.IndexAny(minorStr, "-+_"); idx > 0 {
			minorStr = minorStr[:idx]
		}
		minor, _ = strconv.Atoi(minorStr)
	}
	return
}

func detectSeccomp() bool {
	// Check if seccomp actions are available via procfs
	data, err := os.ReadFile("/proc/sys/kernel/seccomp/actions_avail")
	if err == nil && len(data) > 0 {
		return true
	}

	// Fallback: kernel 3.5+ with CONFIG_SECCOMP should have /proc/self/status with Seccomp field
	data, err = os.ReadFile("/proc/self/status")
	if err == nil {
		return strings.Contains(string(data), "Seccomp:")
	}

	return false
}

func detectLandlock() (available bool, abi int) {
	// Landlock requires kernel 5.13+. Quick check first.
	var uname syscall.Utsname
	if err := syscall.Uname(&uname); err == nil {
		major, minor := parseKernelVersion(utsToString(uname.Release))
		if major < 5 || (major == 5 && minor < 13) {
			return false, 0
		}
	}

	// Probe Landlock ABI via syscall
	// landlock_create_ruleset(NULL, 0, LANDLOCK_CREATE_RULESET_VERSION)
	// SYS_landlock_create_ruleset = 444 on x86_64
	const sysLandlockCreateRuleset = 444
	const landlockCreateRulesetVersion = 1 << 0

	ret, _, errno := syscall.Syscall(sysLandlockCreateRuleset, 0, 0, landlockCreateRulesetVersion)
	if errno == 0 || errno == syscall.ENOSYS {
		if errno == syscall.ENOSYS {
			return false, 0
		}
		return true, int(ret)
	}

	// EOPNOTSUPP means landlock is compiled but disabled
	if errno == syscall.EOPNOTSUPP {
		return false, 0
	}

	return false, 0
}

func detectCgroupVersion() int {
	// cgroups v2: unified hierarchy at /sys/fs/cgroup with cgroup.controllers
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		return 2
	}

	// cgroups v1: per-controller directories
	if entries, err := os.ReadDir("/sys/fs/cgroup"); err == nil {
		for _, e := range entries {
			if e.IsDir() && (e.Name() == "memory" || e.Name() == "pids" || e.Name() == "cpu") {
				return 1
			}
		}
	}

	return 0
}

func detectUserNS() bool {
	// Check max_user_namespaces > 0
	data, err := os.ReadFile("/proc/sys/user/max_user_namespaces")
	if err != nil {
		return false
	}
	val, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return val > 0
}
