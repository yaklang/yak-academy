package service

import (
	"context"
	"strings"
	"time"
)

// 公开证书验证：仅凭验证码读取证书快照，不返回账号 ID/用户名等内部标识。
// 页面与接口都不需要登录；验证码为 16 位大写十六进制，大小写不敏感。
type CertificateVerificationView struct {
	Valid           bool       `json:"valid"`
	Code            string     `json:"code"`
	Title           string     `json:"title"`
	Level           string     `json:"level"`
	CompetitionName string     `json:"competition_name"`
	RecipientName   string     `json:"recipient_name"`
	Rank            int        `json:"rank"`
	Score           int        `json:"score"`
	IssuedAt        *time.Time `json:"issued_at"`
}

func (s *CertificateService) VerifyCertificate(ctx context.Context, code string) (*CertificateVerificationView, error) {
	normalized := strings.ToUpper(strings.TrimSpace(code))
	if normalized == "" || len(normalized) > 32 {
		return nil, ErrNotFound
	}
	cert, err := s.comps.FindCertificateByCode(ctx, normalized)
	if err != nil {
		return nil, err
	}
	if cert == nil {
		return nil, ErrNotFound
	}
	rule, err := s.comps.FindCertificateRule(ctx, cert.RuleID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	comp, err := s.comps.FindByID(ctx, cert.CompetitionID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	return &CertificateVerificationView{
		Valid: true, Code: cert.Code, Title: rule.Title, Level: cert.Level,
		CompetitionName: comp.Name, RecipientName: cert.RecipientName,
		Rank: cert.Rank, Score: cert.Score, IssuedAt: rule.IssuedAt,
	}, nil
}
