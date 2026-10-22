package repo

// PostgreSQL 方言兼容性集成测试：真实 PG 实例上跑 AutoMigrate + 全部一次性
// 迁移 + 方言敏感的派生表路径（upsert / 布尔迁移 / JSON 文本列过滤）。
// 开启方式二选一，默认 skip（常规单测零外部依赖）：
//   - ACADEMY_TEST_PG_DSN 直连（如 SSH 隧道后的实例）
//   - ACADEMY_TEST_DOCKER=1 本地起一次性 postgres:18-alpine 容器

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
)

func TestPostgresCompatDerivedPaths(t *testing.T) {
	dsn := os.Getenv("ACADEMY_TEST_PG_DSN")
	if dsn == "" {
		if os.Getenv("ACADEMY_TEST_DOCKER") != "1" {
			t.Skip("需真实 PostgreSQL：设 ACADEMY_TEST_PG_DSN 或 ACADEMY_TEST_DOCKER=1")
		}
		dsn = startPostgresContainer(t)
	}

	gdb, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	// AutoMigrate 全实体 + 全部一次性迁移（含参数化布尔 SQL 的方言验证）。
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate on postgres: %v", err)
	}
	// 迁移幂等（二次执行不因方言报错）。
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	ctx := context.Background()
	events := NewEventRepo(gdb)
	scores := NewScoreRepo(gdb)
	progress := NewProgressRepo(gdb)

	// Event append-only + ListAfter。
	u1, u2 := uint(41001), uint(41002)
	ev1 := &model.Event{UserID: u1, Type: model.EventHintUnlocked, Payload: `{"hint_id":12,"challenge_id":7}`}
	ev2 := &model.Event{UserID: u1, Type: model.EventHintUnlocked, Payload: `{"hint_id":1,"challenge_id":8}`}
	if err := events.Append(ctx, ev1); err != nil {
		t.Fatalf("append ev1: %v", err)
	}
	if err := events.Append(ctx, ev2); err != nil {
		t.Fatalf("append ev2: %v", err)
	}
	list, err := events.ListAfter(ctx, 0, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("list after: %v n=%d", err, len(list))
	}

	// HasHintUnlocked：LIKE 预筛 + Go 精确解析（hint 1 与 12 不得互撞）。
	if ok, err := events.HasHintUnlocked(ctx, u1, 1); err != nil || !ok {
		t.Fatalf("hint 1 unlocked: ok=%v err=%v", ok, err)
	}
	if ok, err := events.HasHintUnlocked(ctx, u1, 12); err != nil || !ok {
		t.Fatalf("hint 12 unlocked: ok=%v err=%v", ok, err)
	}
	if ok, err := events.HasHintUnlocked(ctx, u1, 13); err != nil || ok {
		t.Fatalf("hint 13 must not match 1/12: ok=%v err=%v", ok, err)
	}
	if ok, err := events.HasHintUnlocked(ctx, u2, 1); err != nil || ok {
		t.Fatalf("other user: ok=%v err=%v", ok, err)
	}

	// Score upsert（ON CONFLICT / excluded 在 PG 原生语义）+ 累加 + 排行榜。
	if err := scores.UpdateScore(ctx, u1, 30, 1); err != nil {
		t.Fatalf("update score 1st: %v", err)
	}
	if err := scores.UpdateScore(ctx, u1, 45, 2); err != nil {
		t.Fatalf("update score 2nd: %v", err)
	}
	rec, err := scores.Get(ctx, u1)
	if err != nil || rec.Score != 75 || rec.SolveCount != 3 {
		t.Fatalf("accumulated score: rec=%+v err=%v", rec, err)
	}
	if _, total, err := scores.Leaderboard(ctx, 0, 10); err != nil || total != 1 {
		t.Fatalf("leaderboard: total=%d err=%v", total, err)
	}
	if n, err := scores.CountAhead(ctx, 74, 0); err != nil || n != 1 {
		t.Fatalf("count ahead: n=%d err=%v", n, err)
	}

	// Progress：FirstOrCreate + 布尔 CASE 聚合。
	if err := progress.MarkInProgress(ctx, u1, "pg-room-a"); err != nil {
		t.Fatalf("mark in progress: %v", err)
	}
	if err := progress.MarkCompleted(ctx, u1, "pg-room-a"); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	if err := progress.MarkInProgress(ctx, u1, "pg-room-b"); err != nil {
		t.Fatalf("mark in progress b: %v", err)
	}
	completed, inProgress, err := progress.Stats(ctx, u1)
	if err != nil || completed != 1 || inProgress != 1 {
		t.Fatalf("progress stats: completed=%d in_progress=%d err=%v", completed, inProgress, err)
	}
}

// startPostgresContainer 本地一次性 postgres 容器（ academy- 前缀名随机端口），
// 就绪探测走容器内 pg_isready。
func startPostgresContainer(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("academy-test-pg-%d", time.Now().UnixNano())
	out, err := exec.Command("docker", "run", "-d", "--rm",
		"--name", name, "-p", "127.0.0.1::5432",
		"-e", "POSTGRES_PASSWORD=academytest", "-e", "POSTGRES_DB=academy_test",
		"postgres:18-alpine").CombinedOutput()
	if err != nil {
		t.Fatalf("run postgres: %v: %s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })

	inspect := exec.Command("docker", "inspect", name, "--format",
		"{{(index (index .NetworkSettings.Ports \"5432/tcp\") 0).HostPort}}")
	portOut, err := inspect.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect port: %v: %s", err, portOut)
	}
	port := string(portOut[:len(portOut)-1]) // 去换行

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.Command("docker", "exec", name, "pg_isready", "-U", "postgres").Run(); err == nil {
			return fmt.Sprintf("postgres://postgres:academytest@127.0.0.1:%s/academy_test?sslmode=disable", port)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("postgres 30s 内未就绪")
	return ""
}

// TestHintPayloadSerializationGuarantee LIKE 预筛依赖 json.Marshal 的无空格
// 序列化（"hint_id":N 子串）：钉住 hint 侧写入格式，防未来改成带空格编码
// 后 LIKE 默默漏配（纯内存，不依赖 PG）。
func TestHintPayloadSerializationGuarantee(t *testing.T) {
	b, err := json.Marshal(map[string]any{"hint_id": 12, "challenge_id": 7})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if s != `{"challenge_id":7,"hint_id":12}` {
		t.Fatalf("payload serialization changed: %s (LIKE 预筛格式前提被打破)", s)
	}
}
