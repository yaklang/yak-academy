package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/service"
)

// mapError 单元测试：内部错误 → 契约错误信封（api-contract skill 错误码表）。
// 501 用例回归防护：曾因重复 case 出现空分支，stub 错误被吞成空 200。

func TestMapErrorNotImplementedReturns501(t *testing.T) {
	rec := httptest.NewRecorder()
	mapError(rec, &service.NotImplementedError{What: "env driver"})
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "NOT_IMPLEMENTED" || env.Error.Message == "" {
		t.Fatalf("envelope = %+v, want NOT_IMPLEMENTED with message", env.Error)
	}
}

func TestMapErrorStatusCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		code string
	}{
		{"gorm record not found", gorm.ErrRecordNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"service not found", service.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"validation with reason", fmt.Errorf("%w: bad zip", service.ErrValidation), http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
		{"has solve history", service.ErrHasSolves, http.StatusConflict, "CONFLICT"},
		{"unknown internal", errors.New("boom"), http.StatusInternalServerError, "INTERNAL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mapError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			var env errorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if env.Error.Code != tc.code {
				t.Fatalf("code = %q, want %q", env.Error.Code, tc.code)
			}
		})
	}
}

func TestMapErrorInstanceConflictCarriesCurrent(t *testing.T) {
	rec := httptest.NewRecorder()
	mapError(rec, &service.InstanceConflictError{
		Current:        &model.LabInstance{ID: 7, ChallengeID: 3},
		Username:       "alice",
		ChallengeTitle: "SQLi basics",
		ChallengeSlug:  "sqli-basics",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var env conflictEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "INSTANCE_CONFLICT" {
		t.Fatalf("code = %q, want INSTANCE_CONFLICT", env.Error.Code)
	}
	if env.Current == nil || env.Current.ChallengeSlug != "sqli-basics" || env.Current.Username != "alice" {
		t.Fatalf("current = %+v, want enriched active instance", env.Current)
	}
}
