package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// 主机/容器性能面板（ops 硬化）：容器管理页顶部展示宿主与全部容器的资源
// 占用快照。数据源三路（与 DockerDriver 同风格走 docker CLI，无 SDK 依赖）：
//   - docker info  --format {{json .}}   → 宿主 CPU/内存总量与容器/镜像计数
//   - docker stats --no-stream --format {{json .}} → 每容器 CPU%/内存/IO/PIDs
//   - Linux 宿主 /proc 采样 + statfs      → 宿主 CPU/内存占用率、数据卷用量
//
// 解析函数全部纯函数（表驱动测试见 perf_test.go）；/proc 与 statfs 为
// Linux-only（部署形态），其他平台返回不可用而不报错。

// HostUsage 宿主自身占用（Linux /proc 采样；容器内 /proc 即宿主全局视图）。
// 不可用平台字段为 nil（前端显示「—」）。
type HostUsage struct {
	CPUPercent  *float64 `json:"cpu_percent"`
	MemTotalBytes int64  `json:"mem_total_bytes"`
	MemUsedBytes  *int64 `json:"mem_used_bytes"`
}

// VolumeUsage 数据卷所在文件系统用量（statfs）。
type VolumeUsage struct {
	Path             string `json:"path"`
	TotalBytes       int64  `json:"total_bytes"`
	UsedBytes        int64  `json:"used_bytes"`
	AvailBytes       int64  `json:"avail_bytes"`
}

// ContainerResource 单容器资源占用（docker stats --no-stream 一行）。
type ContainerResource struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	CPUPercent       float64  `json:"cpu_percent"`
	MemUsedBytes     int64    `json:"mem_used_bytes"`
	MemLimitBytes    int64    `json:"mem_limit_bytes"`
	MemPercent       float64  `json:"mem_percent"`
	NetIOBytes       [2]*int64 `json:"net_io_bytes"`       // 收/发；解析失败为 nil
	BlockIOBytes     [2]*int64 `json:"block_io_bytes"`     // 读/写；解析失败为 nil
	PIDs             int      `json:"pids"`
}

// HostStats 性能面板整体快照。
type HostStats struct {
	NCPU               int                `json:"ncpu"`
	MemTotalBytes      int64              `json:"mem_total_bytes"`
	ContainersTotal    int                `json:"containers_total"`
	ContainersRunning  int                `json:"containers_running"`
	ContainersPaused   int                `json:"containers_paused"`
	ContainersStopped  int                `json:"containers_stopped"`
	Images             int                `json:"images"`
	Host               *HostUsage         `json:"host"`
	DataVolume         *VolumeUsage       `json:"data_volume"`
	Containers         []ContainerResource `json:"containers"`
}

// dockerInfoFields docker info JSON 输出中本面板关心的字段（其余忽略）。
type dockerInfoFields struct {
	NCPU              int   `json:"NCPU"`
	MemTotal          int64 `json:"MemTotal"`
	Containers        int   `json:"Containers"`
	ContainersRunning int   `json:"ContainersRunning"`
	ContainersPaused  int   `json:"ContainersPaused"`
	ContainersStopped int   `json:"ContainersStopped"`
	Images            int   `json:"Images"`
}

// statsLine docker stats --format {{json .}} 的单行输出。
type statsLine struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	MemPerc  string `json:"MemPerc"`
	NetIO    string `json:"NetIO"`
	BlockIO  string `json:"BlockIO"`
	PIDs     string `json:"PIDs"`
}

// HostStats 采集性能快照：docker info + docker stats + Linux /proc 采样。
// CPU 占用率需两次采样（200ms 窗口）权衡精度与接口延迟（面板 10s 级刷新）。
func (d DockerDriver) HostStats(ctx context.Context) (*HostStats, error) {
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{json .}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker info: %v: %s", err, strings.TrimSpace(string(out)))
	}
	stats, err := hostStatsFromInfo(out)
	if err != nil {
		return nil, err
	}

	if out, err = exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker stats: %v: %s", err, strings.TrimSpace(string(out)))
	}
	stats.Containers = parseStatsOutput(string(out))
	if total, used, ok := hostMemory(); ok {
		stats.Host = &HostUsage{MemTotalBytes: total, MemUsedBytes: &used}
	} else {
		stats.Host = &HostUsage{MemTotalBytes: stats.MemTotalBytes}
	}
	if busy1, total1, ok := sampleCPUTicks(); ok {
		// 采样窗口内 ctx 不感知（time.Sleep）；200ms 量级可接受。
		time.Sleep(200 * time.Millisecond)
		if busy2, total2, ok := sampleCPUTicks(); ok {
			pct := cpuPercent(busy1, total1, busy2, total2)
			stats.Host.CPUPercent = &pct
		}
	}
	return stats, nil
}

// hostStatsFromInfo 解析 docker info JSON（纯函数便于测试）。
func hostStatsFromInfo(raw []byte) (*HostStats, error) {
	var info dockerInfoFields
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("docker info json: %v", err)
	}
	return &HostStats{
		NCPU:              info.NCPU,
		MemTotalBytes:     info.MemTotal,
		ContainersTotal:   info.Containers,
		ContainersRunning: info.ContainersRunning,
		ContainersPaused:  info.ContainersPaused,
		ContainersStopped: info.ContainersStopped,
		Images:            info.Images,
		Containers:        []ContainerResource{},
	}, nil
}

// parseStatsOutput 解析 docker stats 按行输出（纯函数便于测试）。
func parseStatsOutput(out string) []ContainerResource {
	var items []ContainerResource
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var sl statsLine
		if err := json.Unmarshal([]byte(line), &sl); err != nil || sl.ID == "" {
			continue
		}
		items = append(items, containerResourceFromLine(sl))
	}
	return items
}

// containerResourceFromLine 单行 stats JSON → 资源行（字符串计量值尽力解析）。
func containerResourceFromLine(sl statsLine) ContainerResource {
	used, limit := parseMemUsage(sl.MemUsage)
	return ContainerResource{
		ID:            sl.ID,
		Name:          sl.Name,
		CPUPercent:    parsePercent(sl.CPUPerc),
		MemUsedBytes:  used,
		MemLimitBytes: limit,
		MemPercent:    parsePercent(sl.MemPerc),
		NetIOBytes:    parseIOPair(sl.NetIO),
		BlockIOBytes:  parseIOPair(sl.BlockIO),
		PIDs:          parseInt(sl.PIDs),
	}
}

// parsePercent 解析 "0.05%" 形式百分比；不可解析返回 0。
func parsePercent(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	if err != nil {
		return 0
	}
	return v
}

// cpuPercent 两次 /proc/stat 采样的 CPU 占用率（纯函数便于测试）；
// 分母非正（时钟回拨/首采样）返回 0。
func cpuPercent(busy1, total1, busy2, total2 uint64) float64 {
	dBusy := int64(busy2) - int64(busy1)
	dTotal := int64(total2) - int64(total1)
	if dTotal <= 0 {
		return 0
	}
	if dBusy < 0 {
		dBusy = 0
	}
	return float64(dBusy) / float64(dTotal) * 100
}

// parseMeminfo 解析 /proc/meminfo 文本（纯函数便于测试）：
// MemTotal/MemAvailable 均为 "键:  值 kB" 行；已用 = total - available。
func parseMeminfo(content string) (total, used int64, ok bool) {
	var totalKb, availKb int64 = -1, -1
	for _, line := range strings.Split(content, "\n") {
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(val))
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(key) {
		case "MemTotal":
			totalKb = v
		case "MemAvailable":
			availKb = v
		}
	}
	if totalKb <= 0 || availKb < 0 {
		return 0, 0, false
	}
	return totalKb * 1024, (totalKb - availKb) * 1024, true
}

// parseProcStat 解析 /proc/stat 聚合 cpu 行（纯函数便于测试）。
// 字段序：user nice system idle iowait irq softirq steal guest guest_nice；
// busy 含 steal（被宿主/其他租户偷走的时间同样是非空闲）。
func parseProcStat(content string) (busy, total uint64, ok bool) {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		if len(fields) < 4 {
			return 0, 0, false
		}
		var vals []uint64
		for _, f := range fields {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			vals = append(vals, v)
		}
		at := func(i int) uint64 {
			if i < len(vals) {
				return vals[i]
			}
			return 0
		}
		idle := at(3) + at(4) // idle + iowait
		busy := at(0) + at(1) + at(2) + at(5) + at(6) + at(7)
		total := busy + idle + at(8) + at(9) // + guest（已计入 user，分母口径对齐）
		return busy, total, true
	}
	return 0, 0, false
}

// parseInt 解析整数字符串（PIDs 等）；不可解析返回 0。
func parseInt(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

// parseMemUsage 解析 "1.2MiB / 512MiB"（docker stats MemUsage）。
func parseMemUsage(s string) (used, limit int64) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	used, _ = parseByteSize(parts[0])
	limit, _ = parseByteSize(parts[1])
	return used, limit
}

// parseIOPair 解析 "1.2kB / 3.4kB"（NetIO/BlockIO）；单边不可解析则该边 nil。
func parseIOPair(s string) [2]*int64 {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return [2]*int64{nil, nil}
	}
	a, aok := parseByteSize(parts[0])
	b, bok := parseByteSize(parts[1])
	var pa, pb *int64
	if aok {
		pa = &a
	}
	if bok {
		pb = &b
	}
	return [2]*int64{pa, pb}
}

// byteUnits docker CLI 计量单位：MemUsage 走二进制（units.BytesSize，
// KiB/MiB/GiB/TiB），NetIO/BlockIO 走十进制（units.HumanSize，kB/MB/GB/TB）。
// 两族统一按单位倍率解析（纯函数便于测试）。
var byteUnits = map[string]int64{
	"B":   1,
	"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
	"kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
}

// byteUnitOrder 单位按长度降序尝试：短单位（B）是长单位（MiB/MB）的后缀，
// 必须先试长的；数值解析失败的单位继续尝试下一个（如 "xMB" 试 B 得 "xM"）。
var byteUnitOrder = func() []string {
	units := make([]string, 0, len(byteUnits))
	for u := range byteUnits {
		units = append(units, u)
	}
	slices.SortFunc(units, func(a, b string) int { return len(b) - len(a) })
	return units
}()

// parseByteSize 解析 "1.2MiB"/"512MiB"/"734B" 形式计量值。
func parseByteSize(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	for _, unit := range byteUnitOrder {
		if !strings.HasSuffix(s, unit) {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, unit)), 64)
		if err != nil {
			continue
		}
		return int64(v * float64(byteUnits[unit])), true
	}
	return 0, false
}
