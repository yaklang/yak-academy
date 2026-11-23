package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"time"
)

// 证书与获奖名单导出：管理端赛后归档。CSV 带 UTF-8 BOM，时间 RFC3339；
// 证书 CSV 含验证码与验证路径，获奖名单 CSV 去掉验证码便于公开张贴。

func certificateCSVWrite(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func certificateRecipientKind(kind string) string {
	if kind == "team" {
		return "队伍"
	}
	return "个人"
}

func certificateCSVTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

// CertificateCSV 证书明细导出（含验证码与公开验证路径）。
func (s *CertificateService) CertificateCSV(ctx context.Context, competitionID uint) ([]byte, string, error) {
	view, err := s.AdminCertificates(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	titles := make(map[uint]string, len(view.Rules))
	for _, rule := range view.Rules {
		titles[rule.ID] = rule.Title
	}
	rows := [][]string{{
		"证书名称", "级别", "接收人类型", "接收人", "用户名",
		"名次", "分数", "验证码", "验证路径", "签发时间",
	}}
	for _, cert := range view.Certificates {
		rows = append(rows, []string{
			titles[cert.RuleID], cert.Level, certificateRecipientKind(cert.RecipientKind),
			cert.RecipientName, cert.Username, strconv.Itoa(cert.Rank), strconv.Itoa(cert.Score),
			cert.Code, "/certificates/" + cert.Code, certificateCSVTime(cert.CreatedAt),
		})
	}
	data, err := certificateCSVWrite(rows)
	if err != nil {
		return nil, "", err
	}
	return data, fmt.Sprintf("competition-%d-certificates.csv", competitionID), nil
}

// AwardCSV 获奖名单导出（不含验证码，供公示/张贴）。
func (s *CertificateService) AwardCSV(ctx context.Context, competitionID uint) ([]byte, string, error) {
	view, err := s.AdminCertificates(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	competition, err := s.comps.FindByID(ctx, competitionID)
	if err != nil {
		return nil, "", mapRecordNotFound(err)
	}
	titles := make(map[uint]string, len(view.Rules))
	for _, rule := range view.Rules {
		titles[rule.ID] = rule.Title
	}
	rows := [][]string{{"竞赛", "奖项", "级别", "接收人类型", "接收人", "名次", "分数"}}
	for _, cert := range view.Certificates {
		rows = append(rows, []string{
			competition.Name, titles[cert.RuleID], cert.Level,
			certificateRecipientKind(cert.RecipientKind),
			cert.RecipientName, strconv.Itoa(cert.Rank), strconv.Itoa(cert.Score),
		})
	}
	data, err := certificateCSVWrite(rows)
	if err != nil {
		return nil, "", err
	}
	return data, fmt.Sprintf("competition-%d-awards.csv", competitionID), nil
}
