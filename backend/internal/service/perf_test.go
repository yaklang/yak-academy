package service

import (
	"strings"
	"testing"
)

// 性能面板解析函数表驱动测试：docker CLI 输出形态与 /proc 文本均固定样例。
// docker stats 计量单位两族并存：MemUsage 二进制（KiB/MiB）、NetIO/BlockIO
// 十进制（kB/MB），parseByteSize 必须两族都认且不被 "B" 短后缀误匹配。

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"734B", 734, true},
		{"1.2MiB", 1258291, true},
		{"512MiB", 512 << 20, true},
		{"2GiB", 2 << 30, true},
		{"1.5kB", 1500, true},
		{"3.25MB", 3250000, true},
		{"1GB", 1e9, true},
		{"2TB", 2e12, true},
		{"", 0, false},
		{"abc", 0, false},
		{"12X", 0, false},
	}
	for _, c := range cases {
		got, ok := parseByteSize(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseByteSize(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseMemUsage(t *testing.T) {
	used, limit := parseMemUsage("1.2MiB / 512MiB")
	if used != 1258291 || limit != 512<<20 {
		t.Errorf("parseMemUsage got (%d, %d)", used, limit)
	}
	used, limit = parseMemUsage("bork")
	if used != 0 || limit != 0 {
		t.Errorf("parseMemUsage malformed got (%d, %d)", used, limit)
	}
}

func TestParseIOPair(t *testing.T) {
	pair := parseIOPair("1.5kB / 3kB")
	if pair[0] == nil || *pair[0] != 1500 || pair[1] == nil || *pair[1] != 3000 {
		t.Errorf("parseIOPair got %v", pair)
	}
	pair = parseIOPair("0B / 0B")
	if pair[0] == nil || *pair[0] != 0 || pair[1] == nil || *pair[1] != 0 {
		t.Errorf("parseIOPair zeros got %v", pair)
	}
	pair = parseIOPair("garbage")
	if pair[0] != nil || pair[1] != nil {
		t.Errorf("parseIOPair malformed got %v", pair)
	}
}

func TestParseStatsOutput(t *testing.T) {
	out := strings.Join([]string{
		`{"BlockIO":"0B / 0B","CPUPerc":"0.05%","Container":"e4f","ID":"e4f2a1","MemPerc":"0.24%","MemUsage":"1.2MiB / 256MiB","Name":"yak-lab-3","NetIO":"1.5kB / 3kB","PIDs":"5"}`,
		``,
		`not-json`,
		`{"BlockIO":"4kB / 8kB","CPUPerc":"12.30%","ID":"ab12","MemPerc":"1.5%","MemUsage":"7.7MiB / 512MiB","Name":"academy-backend","NetIO":"2.1MB / 1MB","PIDs":"42"}`,
	}, "\n")
	items := parseStatsOutput(out)
	if len(items) != 2 {
		t.Fatalf("parseStatsOutput got %d items, want 2", len(items))
	}
	first := items[0]
	if first.ID != "e4f2a1" || first.Name != "yak-lab-3" {
		t.Errorf("first identity got %+v", first)
	}
	if first.CPUPercent != 0.05 || first.MemPercent != 0.24 || first.PIDs != 5 {
		t.Errorf("first scalars got %+v", first)
	}
	if first.MemUsedBytes != 1258291 || first.MemLimitBytes != 256<<20 {
		t.Errorf("first mem got (%d, %d)", first.MemUsedBytes, first.MemLimitBytes)
	}
	if first.NetIOBytes[0] == nil || *first.NetIOBytes[0] != 1500 {
		t.Errorf("first net read got %v", first.NetIOBytes[0])
	}
	second := items[1]
	if second.Name != "academy-backend" || second.CPUPercent != 12.30 {
		t.Errorf("second got %+v", second)
	}
}

func TestHostStatsFromInfo(t *testing.T) {
	raw := []byte(`{"NCPU":8,"MemTotal":16777216000,"Containers":9,"ContainersRunning":7,"ContainersPaused":0,"ContainersStopped":2,"Images":13,"Other":"ignored"}`)
	stats, err := hostStatsFromInfo(raw)
	if err != nil {
		t.Fatalf("hostStatsFromInfo: %v", err)
	}
	if stats.NCPU != 8 || stats.MemTotalBytes != 16777216000 {
		t.Errorf("host basics got %+v", stats)
	}
	if stats.ContainersTotal != 9 || stats.ContainersRunning != 7 || stats.ContainersStopped != 2 || stats.Images != 13 {
		t.Errorf("host counts got %+v", stats)
	}
	if stats.Containers == nil {
		t.Errorf("containers list should be non-nil empty slice")
	}
	if _, err := hostStatsFromInfo([]byte(`nope`)); err == nil {
		t.Errorf("malformed docker info json should error")
	}
}

func TestCPUPercent(t *testing.T) {
	// busy 增量 25、total 增量 100 → 25%。
	if got := cpuPercent(100, 400, 125, 500); got != 25 {
		t.Errorf("cpuPercent got %v, want 25", got)
	}
	if got := cpuPercent(50, 100, 40, 90); got != 0 {
		t.Errorf("cpuPercent non-positive delta got %v", got)
	}
}

func TestParseMeminfo(t *testing.T) {
	content := "MemTotal:       16384000 kB\nMemFree:         8000000 kB\nMemAvailable:   12000000 kB\nBuffers:          100000 kB\n"
	total, used, ok := parseMeminfo(content)
	if !ok {
		t.Fatalf("parseMeminfo should parse")
	}
	if total != 16384000*1024 || used != (16384000-12000000)*1024 {
		t.Errorf("parseMeminfo got (%d, %d)", total, used)
	}
	if _, _, ok := parseMeminfo("garbage"); ok {
		t.Errorf("parseMeminfo malformed should not parse")
	}
}

func TestParseProcStat(t *testing.T) {
	// user nice system idle iowait irq softirq steal guest guest_nice
	content := "cpu  100 0 50 700 20 10 5 15 0 0\n cpu0 50 0 25 350 10 5 2 7 0 0\nintr 12345\n"
	busy, total, ok := parseProcStat(content)
	if !ok {
		t.Fatalf("parseProcStat should parse")
	}
	// busy = 100+0+50+10+5+15 = 180；idle = 700+20 = 720；total = 180+720 = 900。
	if busy != 180 || total != 900 {
		t.Errorf("parseProcStat got (%d, %d), want (180, 900)", busy, total)
	}
	if _, _, ok := parseProcStat("intr 1\n"); ok {
		t.Errorf("parseProcStat without cpu line should not parse")
	}
}
