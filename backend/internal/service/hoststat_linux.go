//go:build linux

package service

import (
	"os"
	"syscall"
)

// Linux 宿主侧采样（性能面板）：容器内 /proc 即宿主全局视图（无 lxcfs 时
// MemTotal/Available 反映宿主内存，/proc/stat 反映宿主全部 CPU），statfs 反映
// 数据卷所在文件系统的真实用量。文本解析在 perf.go（平台无关，表驱动测试）。

// hostMemory 读 /proc/meminfo：总内存与已用（total - available）。
func hostMemory() (total, used int64, ok bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	return parseMeminfo(string(data))
}

// sampleCPUTicks 读 /proc/stat 首行（cpu 聚合行）：
// busy = user+nice+system+irq+softirq+steal，total = busy + idle+iowait+guest。
func sampleCPUTicks() (busy, total uint64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	return parseProcStat(string(data))
}

// volumeUsage statfs 数据卷用量（非 Linux 开发态返回 nil，见 hoststat_other.go）。
func volumeUsage(path string) *VolumeUsage {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return nil
	}
	bsize := int64(st.Bsize)
	total := int64(st.Blocks) * bsize
	return &VolumeUsage{
		Path:       path,
		TotalBytes: total,
		UsedBytes:  total - int64(st.Bfree)*bsize,
		AvailBytes: int64(st.Bavail) * bsize,
	}
}
