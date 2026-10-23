//go:build !linux

package service

// 非 Linux 平台（本地 Windows/macOS 开发）无 /proc 与 statfs 对应视图：
// 宿主占用与数据卷用量返回不可用，面板仍显示 docker info/stats 数据。
// 部署形态仅支持 Linux 宿主（architecture.md §11），此处仅为开发态降级。

func hostMemory() (total, used int64, ok bool) { return 0, 0, false }

func sampleCPUTicks() (busy, total uint64, ok bool) { return 0, 0, false }

func volumeUsage(path string) *VolumeUsage { return nil }
