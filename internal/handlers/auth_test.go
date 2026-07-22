package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func reqWith(admin bool, claimCompany string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/workspace/tree", nil)
	ctx := context.WithValue(r.Context(), ContextUserID, "user-1")
	ctx = context.WithValue(ctx, ContextIsAdmin, admin)
	ctx = context.WithValue(ctx, ContextCompanyID, claimCompany)
	return r.WithContext(ctx)
}

func TestRequireCompanyAccess(t *testing.T) {
	companyA := "11111111-1111-1111-1111-111111111111"
	companyB := "22222222-2222-2222-2222-222222222222"

	cases := []struct {
		name      string
		admin     bool
		claim     string
		requested string
		wantAllow bool
	}{
		{"admin (api key or dev2-admins) unrestricted", true, "", companyB, true},
		{"matching claim allowed", false, companyA, companyA, true},
		{"mismatched claim denied", false, companyA, companyB, false},
		{"missing claim denied", false, "", companyA, false},
		{"empty requested company denied for non-admin", false, companyA, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			got := RequireCompanyAccess(rec, reqWith(tc.admin, tc.claim), tc.requested)
			if got != tc.wantAllow {
				t.Fatalf("RequireCompanyAccess = %v, want %v", got, tc.wantAllow)
			}
			if !tc.wantAllow && rec.Code != http.StatusForbidden {
				t.Fatalf("denied request status = %d, want 403", rec.Code)
			}
		})
	}
}

func TestGetCompanyID(t *testing.T) {
	if got := GetCompanyID(reqWith(false, "company-x")); got != "company-x" {
		t.Fatalf("GetCompanyID = %q, want %q", got, "company-x")
	}
	if got := GetCompanyID(httptest.NewRequest(http.MethodGet, "/", nil)); got != "" {
		t.Fatalf("GetCompanyID on bare request = %q, want empty", got)
	}
}
