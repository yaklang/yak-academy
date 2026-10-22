package service

// S3 后端对真实 MinIO 的受控集成测试：ACADEMY_TEST_DOCKER=1 且 docker 可用时
// 起一次性 minio 容器跑完整往返（Save/Open/Remove/RemovePrefix/ErrNotFound/
// Prefix 键映射），默认 skip（project-conventions：常规单测零外部依赖）。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

const (
	minioTestUser = "academytest"
	minioTestPass = "academytest-secret"
)

// S3WriteupStoreRoundtrip against real MinIO:
//   - 默认（ACADEMY_TEST_DOCKER=1 且 docker 可用）：本地起一次性 minio 容器；
//   - ACADEMY_TEST_MINIO_ENDPOINT 直连模式：指向现成 MinIO（如 SSH 隧道后的
//     远端实例），凭据经 ACADEMY_TEST_MINIO_USER/_PASS 覆盖（默认开发值）。
// 两种模式二选一，都不满足则 skip（常规单测零外部依赖）。
func TestS3WriteupStoreRoundtripAgainstMinIO(t *testing.T) {
	endpoint := os.Getenv("ACADEMY_TEST_MINIO_ENDPOINT")
	user := os.Getenv("ACADEMY_TEST_MINIO_USER")
	pass := os.Getenv("ACADEMY_TEST_MINIO_PASS")
	if user == "" {
		user, pass = minioTestUser, minioTestPass
	}

	if endpoint == "" {
		ctx := requireDocker(t)
		name := fmt.Sprintf("academy-test-minio-%d", time.Now().UnixNano())
		run := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
			"--name", name, "-p", "127.0.0.1::9000",
			"-e", "MINIO_ROOT_USER="+user,
			"-e", "MINIO_ROOT_PASSWORD="+pass,
			"minio/minio:latest", "server", "/data")
		if out, err := run.CombinedOutput(); err != nil {
			t.Fatalf("run minio: %v: %s", err, out)
		}
		t.Cleanup(func() {
			exec.Command("docker", "rm", "-f", name).Run()
		})
		port := dockerInspectFormat(ctx, t, name, "{{(index (index .NetworkSettings.Ports \"9000/tcp\") 0).HostPort}}")
		endpoint = "http://127.0.0.1:" + port
	}
	ctx := context.Background()

	store, err := NewWriteupStore("s3", "", S3StoreOptions{
		Endpoint: endpoint, Bucket: "academy-writeup-test", AccessKey: user, SecretKey: pass,
		Prefix: "academy",
	})
	if err != nil {
		t.Fatalf("build store: %v", err)
	}
	s3 := store.(*s3WriteupStore)
	bucket := "academy-writeup-test"
	if err := s3.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		if exists, _ := s3.client.BucketExists(ctx, bucket); !exists {
			t.Fatalf("make bucket: %v", err)
		}
	}
	waitMinIOReady(t, s3)

	// 键映射：Prefix + rel。
	if err := store.Save(ctx, "12/5.zip", []byte("PK\x03\x04-zip")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := s3.client.StatObject(ctx, bucket, "academy/12/5.zip", minio.StatObjectOptions{}); err != nil {
		t.Fatalf("key must be prefix-scoped: %v", err)
	}
	r, size, err := store.Open(ctx, "12/5.zip")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, []byte("PK\x03\x04-zip")) || size != int64(len("PK\x03\x04-zip")) {
		t.Fatalf("roundtrip: %q size=%d", got, size)
	}

	// 不存在 → ErrNotFound。
	if _, _, err := store.Open(ctx, "12/404.zip"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing open: %v", err)
	}

	// RemovePrefix 只清目标竞赛前缀（Prefix 之内）。
	if err := store.Save(ctx, "13/1.zip", []byte("a")); err != nil {
		t.Fatalf("save sibling: %v", err)
	}
	if err := store.RemovePrefix(ctx, "12"); err != nil {
		t.Fatalf("remove prefix: %v", err)
	}
	if _, _, err := store.Open(ctx, "12/5.zip"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("prefix removed: %v", err)
	}
	if _, _, err := store.Open(ctx, "13/1.zip"); err != nil {
		t.Fatalf("sibling survived: %v", err)
	}

	// Remove 幂等。
	if err := store.Remove(ctx, "13/1.zip"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := store.Remove(ctx, "13/1.zip"); err != nil {
		t.Fatalf("remove again: %v", err)
	}
}

// waitMinIOReady MinIO 起容器后首次请求前有小窗口，轮询桶列表到通。
func waitMinIOReady(t *testing.T, s3 *s3WriteupStore) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := s3.client.ListBuckets(ctx)
		cancel()
		if err == nil {
			return
		}
		if !strings.Contains(err.Error(), "connection refused") && ctx.Err() == nil {
			t.Fatalf("minio not ready in unexpected way: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("minio 20s 内未就绪")
}
