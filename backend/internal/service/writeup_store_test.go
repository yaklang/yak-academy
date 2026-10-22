package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestFSWriteupStoreRoundtrip(t *testing.T) {
	ctx := context.Background()
	store := NewFSWriteupStore(t.TempDir())

	if err := store.Save(ctx, "7/3.zip", []byte("PK\x03\x04-v1")); err != nil {
		t.Fatalf("save: %v", err)
	}
	r, size, err := store.Open(ctx, "7/3.zip")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, []byte("PK\x03\x04-v1")) || size != int64(len("PK\x03\x04-v1")) {
		t.Fatalf("roundtrip: %q size=%d", got, size)
	}

	// 覆盖替换（同键再写）。
	if err := store.Save(ctx, "7/3.zip", []byte("PK\x03\x04-v2")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	orc, size, err := store.Open(ctx, "7/3.zip")
	if err != nil || size != int64(len("PK\x03\x04-v2")) {
		orc.Close()
		t.Fatalf("overwrite open: size=%d err=%v", size, err)
	}
	orc.Close()

	// 不存在 → 统一 ErrNotFound。
	if _, _, err := store.Open(ctx, "7/404.zip"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing open: %v", err)
	}

	// Remove 幂等：删一次成功、再删不报错。
	if err := store.Remove(ctx, "7/3.zip"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := store.Remove(ctx, "7/3.zip"); err != nil {
		t.Fatalf("remove again: %v", err)
	}
	if _, _, err := store.Open(ctx, "7/3.zip"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed open: %v", err)
	}
}

func TestFSWriteupStoreRemovePrefixCascades(t *testing.T) {
	ctx := context.Background()
	store := NewFSWriteupStore(t.TempDir())
	for _, rel := range []string{"9/1.zip", "9/2.zip", "10/1.zip"} {
		if err := store.Save(ctx, rel, []byte("x")); err != nil {
			t.Fatalf("save %s: %v", rel, err)
		}
	}
	if err := store.RemovePrefix(ctx, "9"); err != nil {
		t.Fatalf("remove prefix: %v", err)
	}
	if _, _, err := store.Open(ctx, "9/1.zip"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("prefix removed: %v", err)
	}
	src, _, err := store.Open(ctx, "10/1.zip")
	if err != nil {
		t.Fatalf("sibling survived: %v", err)
	}
	src.Close()
	// 不存在的前缀不算错。
	if err := store.RemovePrefix(ctx, "404"); err != nil {
		t.Fatalf("missing prefix: %v", err)
	}
}

func TestFSWriteupStoreRejectsUnsafeRel(t *testing.T) {
	store := NewFSWriteupStore(t.TempDir())
	if _, _, err := store.Open(context.Background(), "../../etc/passwd"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unsafe open: %v", err)
	}
	if err := store.Remove(context.Background(), "../escape"); err != nil {
		t.Fatalf("unsafe remove should be a no-op success: %v", err)
	}
}

func TestNewWriteupStoreFactory(t *testing.T) {
	if _, err := NewWriteupStore("", t.TempDir(), S3StoreOptions{}); err != nil {
		t.Fatalf("default fs: %v", err)
	}
	if _, err := NewWriteupStore("fs", t.TempDir(), S3StoreOptions{}); err != nil {
		t.Fatalf("fs: %v", err)
	}
	// s3 缺端点/桶 → 配置错误（ErrValidation），fail fast。
	if _, err := NewWriteupStore("s3", "", S3StoreOptions{Bucket: "b"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("s3 missing endpoint: %v", err)
	}
	if _, err := NewWriteupStore("s3", "", S3StoreOptions{Endpoint: "minio:9000"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("s3 missing bucket: %v", err)
	}
	if _, err := NewWriteupStore("gcs", "", S3StoreOptions{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown kind: %v", err)
	}
}

func TestNormalizeS3Endpoint(t *testing.T) {
	cases := []struct{ in, want string }{
		{"minio:9000", "minio:9000"},
		{"http://minio:9000", "minio:9000"},
		{"https://s3.example.com/", "s3.example.com"},
		{"minio:9000/prefix", "minio:9000/prefix"},
	}
	for _, c := range cases {
		got, err := normalizeS3Endpoint(c.in)
		if err != nil || got != c.want {
			t.Fatalf("normalize(%q): got %q err=%v want %q", c.in, got, err, c.want)
		}
	}
	if _, err := normalizeS3Endpoint("http:///nohost"); err == nil {
		t.Fatal("no-host endpoint should fail")
	}
	if isHTTPSEndpoint("http://minio:9000") {
		t.Fatal("http:// must not be TLS")
	}
	if !isHTTPSEndpoint("https://minio:9000") || !isHTTPSEndpoint("minio:9000") {
		t.Fatal("https:// or schemeless must be TLS")
	}
}
