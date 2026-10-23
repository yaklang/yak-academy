package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// AuditService 管理操作审计（ops 硬化）：管理端写操作逐条落 admin_audit_logs
//（append-only）。与 Event 表分工见 model.AdminAuditLog——这里不做事件溯源，
// 只提供「谁在何时对什么做了什么」的不可改视图。
type AuditService struct {
	logs repo.AuditLogRepo
}

func NewAuditService(logs repo.AuditLogRepo) *AuditService {
	return &AuditService{logs: logs}
}

// Record 记一条管理操作。detail 为任意可 JSON 序列化的附加信息（nil = 无）。
// ip 传 RemoteAddr 原文即可（内部剥端口留 host）。
func (s *AuditService) Record(ctx context.Context, admin *model.User, action, targetType, targetID, ip string, detail any) error {
	if admin == nil {
		return fmt.Errorf("%w: audit record requires an authenticated admin", ErrValidation)
	}
	l := &model.AdminAuditLog{
		AdminID: admin.ID, AdminName: admin.Username,
		Action: action, TargetType: targetType, TargetID: targetID,
		IP:        hostOnly(ip),
		CreatedAt: time.Now().UTC(),
	}
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("audit detail marshal: %w", err)
		}
		l.Detail = string(b)
	}
	return s.logs.Create(ctx, l)
}

// AuditLogView 列表行（Detail 解析为 JSON 对象下发，前端免二次解析）。
type AuditLogView struct {
	model.AdminAuditLog
	// Detail 覆写：空串 → null，否则原样 JSON（RawMessage 不再转义）。
	Detail json.RawMessage `json:"detail"`
}

// List 审计分页查询（新→旧；可按管理员/动作过滤）。
func (s *AuditService) List(ctx context.Context, f repo.AuditFilter, offset, limit int) ([]AuditLogView, int64, error) {
	items, total, err := s.logs.List(ctx, f, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AuditLogView, len(items))
	for i, it := range items {
		v := AuditLogView{AdminAuditLog: it}
		if it.Detail != "" {
			v.Detail = json.RawMessage(it.Detail)
		}
		out[i] = v
	}
	return out, total, nil
}

// hostOnly 剥 RemoteAddr 的端口（IPv6 [::1]:x / IPv4 x:y 都兼容；非地址原文返回）。
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
