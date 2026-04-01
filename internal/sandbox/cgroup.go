package sandbox

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CgroupManager manages cgroup resource limits for subprocesses.
// Supports both cgroups v1 (per-controller) and v2 (unified hierarchy).
type CgroupManager struct {
	version int
	plan    *EnforcementPlan
}

func NewCgroupManager(caps PlatformCapabilities, plan *EnforcementPlan) *CgroupManager {
	return &CgroupManager{
		version: caps.CgroupVersion,
		plan:    plan,
	}
}

// Available returns whether cgroup enforcement is possible.
func (cm *CgroupManager) Available() bool {
	return cm.version > 0
}

// CreateForPID creates a cgroup for a specific subprocess PID with the
// resource limits from the enforcement plan.
// Returns a cleanup function that removes the cgroup.
func (cm *CgroupManager) CreateForPID(pid int, label string) (cleanup func(), err error) {
	if !cm.Available() {
		return func() {}, nil
	}

	cgroupName := fmt.Sprintf("tc-%s-%d", label, pid)

	switch cm.version {
	case 2:
		return cm.createV2(pid, cgroupName)
	case 1:
		return cm.createV1(pid, cgroupName)
	default:
		return func() {}, nil
	}
}

// createV2 creates a cgroup in the v2 unified hierarchy.
func (cm *CgroupManager) createV2(pid int, name string) (func(), error) {
	base := "/sys/fs/cgroup"
	cgPath := filepath.Join(base, "tc", name)

	if err := os.MkdirAll(cgPath, 0755); err != nil {
		return func() {}, fmt.Errorf("create cgroup v2 %s: %w", cgPath, err)
	}

	cleanup := func() {
		os.Remove(cgPath)
		os.Remove(filepath.Join(base, "tc"))
	}

	// Memory limit
	if cm.plan.MemoryLimitBytes > 0 {
		writeFile(filepath.Join(cgPath, "memory.max"),
			strconv.FormatInt(cm.plan.MemoryLimitBytes, 10))
	}

	// CPU limit (as period/quota)
	if cm.plan.CPUCores > 0 {
		period := 100000 // 100ms
		quota := period * cm.plan.CPUCores
		writeFile(filepath.Join(cgPath, "cpu.max"),
			fmt.Sprintf("%d %d", quota, period))
	}

	// PID limit
	if cm.plan.MaxPidsPerSubprocess > 0 {
		writeFile(filepath.Join(cgPath, "pids.max"),
			strconv.Itoa(cm.plan.MaxPidsPerSubprocess))
	}

	// Add process to cgroup
	writeFile(filepath.Join(cgPath, "cgroup.procs"), strconv.Itoa(pid))

	log.Printf("cgroup v2: created %s for pid %d (mem=%dMB, cpu=%d, pids=%d)",
		name, pid, cm.plan.MemoryLimitBytes/(1024*1024),
		cm.plan.CPUCores, cm.plan.MaxPidsPerSubprocess)

	return cleanup, nil
}

// createV1 creates cgroups in the v1 per-controller hierarchy.
func (cm *CgroupManager) createV1(pid int, name string) (func(), error) {
	var cleanups []func()

	// Memory controller
	if cm.plan.MemoryLimitBytes > 0 {
		memPath := filepath.Join("/sys/fs/cgroup/memory/tc", name)
		if err := os.MkdirAll(memPath, 0755); err == nil {
			writeFile(filepath.Join(memPath, "memory.limit_in_bytes"),
				strconv.FormatInt(cm.plan.MemoryLimitBytes, 10))
			writeFile(filepath.Join(memPath, "cgroup.procs"), strconv.Itoa(pid))
			cleanups = append(cleanups, func() {
				os.Remove(memPath)
				os.Remove(filepath.Join("/sys/fs/cgroup/memory", "tc"))
			})
		} else {
			log.Printf("cgroup v1: cannot create memory cgroup: %v (may need permissions)", err)
		}
	}

	// PIDs controller
	if cm.plan.MaxPidsPerSubprocess > 0 {
		pidsPath := filepath.Join("/sys/fs/cgroup/pids/tc", name)
		if err := os.MkdirAll(pidsPath, 0755); err == nil {
			writeFile(filepath.Join(pidsPath, "pids.max"),
				strconv.Itoa(cm.plan.MaxPidsPerSubprocess))
			writeFile(filepath.Join(pidsPath, "cgroup.procs"), strconv.Itoa(pid))
			cleanups = append(cleanups, func() {
				os.Remove(pidsPath)
				os.Remove(filepath.Join("/sys/fs/cgroup/pids", "tc"))
			})
		} else {
			log.Printf("cgroup v1: cannot create pids cgroup: %v (may need permissions)", err)
		}
	}

	// CPU controller
	if cm.plan.CPUCores > 0 {
		cpuPath := filepath.Join("/sys/fs/cgroup/cpu,cpuacct/tc", name)
		if err := os.MkdirAll(cpuPath, 0755); err == nil {
			period := 100000
			quota := period * cm.plan.CPUCores
			writeFile(filepath.Join(cpuPath, "cpu.cfs_period_us"),
				strconv.Itoa(period))
			writeFile(filepath.Join(cpuPath, "cpu.cfs_quota_us"),
				strconv.Itoa(quota))
			writeFile(filepath.Join(cpuPath, "cgroup.procs"), strconv.Itoa(pid))
			cleanups = append(cleanups, func() {
				os.Remove(cpuPath)
				os.Remove(filepath.Join("/sys/fs/cgroup/cpu,cpuacct", "tc"))
			})
		} else {
			log.Printf("cgroup v1: cannot create cpu cgroup: %v (may need permissions)", err)
		}
	}

	cleanup := func() {
		for _, fn := range cleanups {
			fn()
		}
	}

	log.Printf("cgroup v1: created %s for pid %d", name, pid)
	return cleanup, nil
}

func writeFile(path, content string) {
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0644); err != nil {
		log.Printf("cgroup: write %s: %v", path, err)
	}
}
