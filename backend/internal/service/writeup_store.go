package service

// WP zip 附件存储抽象：fs 本地目录（默认）与 S3 兼容对象存储（见
// writeup_store_s3.go）两个后端，NewWriteupStore 按配置装配。
// rel/key 形如 <competitionID>/<wpID>.zip——段全部由服务端数字 ID 拼出，
// 路径穿越面只来自库内历史 ZipFile 值，fs 后端经 pathutil.SafeJoin 兜底。

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/yaklang/yak-academy/backend/internal/pathutil"
)

// WriteupStore WP zip 附件存储。Open 返回 (流, 字节数)；不存在统一映射
// ErrNotFound（api 层转 404），其余错误原样上抛。
type WriteupStore interface {
	Save(ctx context.Context, rel string, data []byte) error
	Open(ctx context.Context, rel string) (io.ReadCloser, int64, error)
	// Remove 删除单个对象；目标不存在视为成功（孤儿清理幂等）。
	Remove(ctx context.Context, rel string) error
	// RemovePrefix 删除前缀下全部对象（竞赛删除级联；空目录/前缀不存在不算错）。
	RemovePrefix(ctx context.Context, prefix string) error
}

// NewWriteupStore 按配置装配 WP 存储后端：fs（默认）/ s3。
func NewWriteupStore(kind, dir string, s3 S3StoreOptions) (WriteupStore, error) {
	switch kind {
	case "", "fs":
		return NewFSWriteupStore(dir), nil
	case "s3":
		return NewS3WriteupStore(s3)
	default:
		return nil, fmt.Errorf("%w: unknown ACADEMY_WRITEUPS_STORE %q (want fs or s3)", ErrValidation, kind)
	}
}

// fsWriteupStore 本地目录后端（原落盘行为原样迁移：MkdirAll + WriteFile）。
type fsWriteupStore struct {
	root string
}

// NewFSWriteupStore 建本地目录后端（root 空 = 相对当前目录，与历史行为一致）。
func NewFSWriteupStore(root string) WriteupStore {
	return &fsWriteupStore{root: root}
}

func (s *fsWriteupStore) Save(_ context.Context, rel string, data []byte) error {
	abs := pathutil.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, data, 0o644)
}

func (s *fsWriteupStore) Open(_ context.Context, rel string) (io.ReadCloser, int64, error) {
	abs, err := pathutil.SafeJoin(s.root, rel)
	if err != nil {
		return nil, 0, ErrNotFound
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, 0, ErrNotFound
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, ErrNotFound
	}
	return f, info.Size(), nil
}

func (s *fsWriteupStore) Remove(_ context.Context, rel string) error {
	abs, err := pathutil.SafeJoin(s.root, rel)
	if err != nil {
		// 库内历史路径不可信形态：无文件可删，孤儿清理视为成功。
		return nil
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *fsWriteupStore) RemovePrefix(_ context.Context, prefix string) error {
	return os.RemoveAll(pathutil.Join(s.root, prefix))
}
