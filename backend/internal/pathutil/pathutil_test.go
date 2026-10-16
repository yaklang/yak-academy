package pathutil

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// CleanEntry 表驱动（纯函数直接测，覆盖三平台来源的条目形态）。
func TestCleanEntry(t *testing.T) {
	cases := []struct {
		name string
		want string
		err  bool
	}{
		// 常规形态（任何平台打包）
		{name: "a.txt", want: "a.txt"},
		{name: "topics/web/lesson.md", want: "topics/web/lesson.md"},
		{name: "src\\main\\app.go", want: "src/main/app.go"},  // Windows 打包的分隔符
		{name: "./a.txt", want: "a.txt"},                      // 相对前缀
		{name: "/a.txt", want: "a.txt"},                       // 前导 / 剥掉（沿用旧语义）
		{name: "dir/", want: "dir"},                           // 目录条目尾部 /
		{name: "a..b.txt", want: "a..b.txt"},                  // 名字里的点不是段
		{name: "__MACOSX/._junk", want: "__MACOSX/._junk"},    // 垃圾条目由调用方策略跳过
		// 拒绝：路径穿越
		{name: "../etc/passwd", err: true},
		{name: "..\\..\\etc\\passwd", err: true},
		{name: "topics/a/../../escape", err: true}, // 中段 .. 同样逃逸（旧实现漏掉的形态）
		{name: "a/./b", err: true},                 // 冗余段按不可信处理
		// 拒绝：绝对形态与空
		{name: "", err: true},
		{name: "/", err: true},
		{name: "./", err: true},
		{name: "a//b", err: true}, // 空段
		// 拒绝：Windows 盘符 / UNC（非 Windows 构建下同样拒绝，zip 可能来自任何平台）
		{name: "C:/windows/system32", err: true},
		{name: "c:\\windows\\system32", err: true},
		{name: "//server/share/f", err: true},
	}
	for _, tc := range cases {
		got, err := CleanEntry(tc.name)
		if tc.err {
			if !errors.Is(err, ErrUnsafePath) {
				t.Errorf("CleanEntry(%q) err = %v, want ErrUnsafePath", tc.name, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("CleanEntry(%q) unexpected err: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CleanEntry(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSafeJoin(t *testing.T) {
	// 干净条目：归一化后按 OS 分隔符落位。
	got, err := SafeJoin(filepath.Join("data", "wr"), "a\\b\\c.txt")
	if err != nil {
		t.Fatalf("SafeJoin: %v", err)
	}
	want := filepath.Join("data", "wr", "a", "b", "c.txt")
	if got != want {
		t.Fatalf("SafeJoin = %q, want %q", got, want)
	}
	if !strings.ContainsRune(got, filepath.Separator) && filepath.Separator == '\\' {
		t.Fatalf("SafeJoin = %q, want OS-native separators on Windows", got)
	}

	// 逃逸条目：报 ErrUnsafePath，绝不返回路径。
	for _, evil := range []string{"../escape.txt", "..\\..\\escape", "C:/sys", "//srv/s"} {
		if p, err := SafeJoin("data", evil); err == nil {
			t.Errorf("SafeJoin(%q) = %q, want ErrUnsafePath", evil, p)
		}
	}
}
