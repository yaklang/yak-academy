package service

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func newAuditTestService(t *testing.T) *AuditService {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:audittestsvc?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	return NewAuditService(repo.NewAuditLogRepo(gdb))
}

func TestAuditRecordMarshalsDetailAndStripsPort(t *testing.T) {
	svc := newAuditTestService(t)
	admin := &model.User{ID: 7, Username: "ops-admin", GlobalRole: model.GlobalRoleAdmin}
	if err := svc.Record(context.Background(), admin, "content.publish", "package", "3",
		"192.168.1.5:52344", map[string]any{"version": 2}); err != nil {
		t.Fatalf("record: %v", err)
	}
	items, total, err := svc.List(context.Background(), repo.AuditFilter{}, 0, 20)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("list = %d items, total %d, err %v", len(items), total, err)
	}
	got := items[0]
	if got.AdminID != 7 || got.AdminName != "ops-admin" || got.Action != "content.publish" ||
		got.TargetType != "package" || got.TargetID != "3" {
		t.Fatalf("unexpected row: %+v", got.AdminAuditLog)
	}
	if got.IP != "192.168.1.5" {
		t.Fatalf("ip = %q, want host without port", got.IP)
	}
	if string(got.Detail) != `{"version":2}` {
		t.Fatalf("detail = %s, want marshaled JSON", got.Detail)
	}
}

func TestAuditListFilters(t *testing.T) {
	svc := newAuditTestService(t)
	a1 := &model.User{ID: 1, Username: "a1"}
	a2 := &model.User{ID: 2, Username: "a2"}
	for _, rec := range []struct {
		admin  *model.User
		action string
	}{
		{a1, "content.publish"}, {a1, "content.publish"}, {a1, "challenge.delete"}, {a2, "content.publish"},
	} {
		if err := svc.Record(context.Background(), rec.admin, rec.action, "x", "1", "127.0.0.1:1", nil); err != nil {
			t.Fatalf("record %v: %v", rec, err)
		}
	}
	// 动作过滤。
	_, total, err := svc.List(context.Background(), repo.AuditFilter{Action: "challenge.delete"}, 0, 20)
	if err != nil || total != 1 {
		t.Fatalf("action filter total = %d, err %v", total, err)
	}
	// 管理员过滤。
	_, total, err = svc.List(context.Background(), repo.AuditFilter{AdminID: 1}, 0, 20)
	if err != nil || total != 3 {
		t.Fatalf("admin filter total = %d, err %v", total, err)
	}
	// 组合过滤。
	_, total, err = svc.List(context.Background(), repo.AuditFilter{AdminID: 2, Action: "content.publish"}, 0, 20)
	if err != nil || total != 1 {
		t.Fatalf("combined filter total = %d, err %v", total, err)
	}
	// nil detail → 下发 null（RawMessage 零值）。
	items, _, err := svc.List(context.Background(), repo.AuditFilter{Action: "challenge.delete"}, 0, 20)
	if err != nil || len(items) != 1 || string(items[0].Detail) != "" {
		t.Fatalf("nil detail row: %+v err %v", items, err)
	}
}

func TestAuditRecordRequiresAdmin(t *testing.T) {
	svc := newAuditTestService(t)
	if err := svc.Record(context.Background(), nil, "x", "y", "z", "127.0.0.1:1", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}
