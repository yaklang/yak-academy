// Package pathutil 磁盘路径统一处理入口（Windows / macOS / Linux 行为一致）。
//
// 仓库路径规则（project-conventions skill「路径处理」）：
//   - 服务层拼磁盘路径一律用本包，业务代码不新写裸 filepath.Join；
//   - 任何来自 zip 条目 / 上传文件名的「不可信路径段」必须经 CleanEntry / SafeJoin，
//     zip slip 防护（拒绝 .. 段、盘符、UNC）集中在这一处；
//   - embed.FS 等包内虚拟路径是 / 分隔的另一套语义，仍用标准库 path.Join，不经本包。
package pathutil

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrUnsafePath 条目名含 .. 段、盘符或 UNC 等不可信形态。
var ErrUnsafePath = errors.New("unsafe path entry")

// CleanEntry 归一化归档/上传条目名为 / 分隔的相对路径（纯函数，不碰磁盘）：
// 统一 \ → /，剥前导 / 与 ./ 前缀，剥结尾单个 /。
// 任一 .. 段、空段（a//b）、Windows 盘符（C:/x）或 UNC（//srv/share）→ ErrUnsafePath。
// zip 可能来自任意平台，盘符/UNC 在非 Windows 构建下同样拒绝。
func CleanEntry(name string) (string, error) {
	p := strings.ReplaceAll(name, "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "", ErrUnsafePath
	}
	if len(p) >= 2 && p[1] == ':' && isLetter(p[0]) || strings.HasPrefix(p, "//") {
		return "", ErrUnsafePath
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", ErrUnsafePath
		}
	}
	return p, nil
}

// SafeJoin 把不可信条目名安全拼进 base 目录：先 CleanEntry 归一化校验，
// 再按当前 OS 分隔符拼接。任何逃逸尝试返回 ErrUnsafePath（不落盘）。
func SafeJoin(base, name string) (string, error) {
	p, err := CleanEntry(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.FromSlash(p)), nil
}

// Join 拼接服务端自有路径段（slug、ID 等非用户可控内容）。
// 语义同 filepath.Join，收口到一处便于统一演进（长度上限、大小写折叠等）。
func Join(elems ...string) string { return filepath.Join(elems...) }

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
