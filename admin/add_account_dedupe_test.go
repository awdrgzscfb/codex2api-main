package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex2api/auth"
	"github.com/codex2api/cache"
	"github.com/codex2api/database"
	"github.com/gin-gonic/gin"
)

func TestAddAccountSkipsRequestAndDatabaseDuplicates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, cleanup := newAddAccountTestHandler(t)
	defer cleanup()

	if _, err := handler.db.InsertAccount(context.Background(), "existing", "rt-existing", ""); err != nil {
		t.Fatalf("InsertAccount(existing): %v", err)
	}
	handler.refreshToken = func(context.Context, string, string) (*auth.TokenData, *auth.AccountInfo, error) {
		return &auth.TokenData{
			AccessToken:  "at-new",
			RefreshToken: "rt-new",
			IDToken:      "id-new",
			ExpiresAt:    time.Unix(1700000000, 0),
		}, &auth.AccountInfo{
			Email:            "new@example.com",
			ChatGPTAccountID: "acct-new",
			PlanType:         "pro",
		}, nil
	}

	body := bytes.NewBufferString("{\"name\":\"batch\",\"refresh_token\":\"rt-existing\\nrt-new\\nrt-new\",\"proxy_url\":\"\"}")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/accounts", body)
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.AddAccount(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := int(payload["success"].(float64)); got != 1 {
		t.Fatalf("success = %d, want %d", got, 1)
	}
	if got := int(payload["duplicate"].(float64)); got != 2 {
		t.Fatalf("duplicate = %d, want %d", got, 2)
	}
	if got := int(payload["failed"].(float64)); got != 0 {
		t.Fatalf("failed = %d, want %d", got, 0)
	}

	rows, err := handler.db.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive(): %v", err)
	}
	if got := len(rows); got != 2 {
		t.Fatalf("active accounts = %d, want %d", got, 2)
	}
}

func TestAddAccountSkipsIdentityDuplicatesAndDoesNotInsertFailedRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, cleanup := newAddAccountTestHandler(t)
	defer cleanup()

	existingID, err := handler.db.InsertAccount(context.Background(), "existing", "rt-existing", "")
	if err != nil {
		t.Fatalf("InsertAccount(existing): %v", err)
	}
	if err := handler.db.UpdateCredentials(context.Background(), existingID, map[string]interface{}{
		"email":      "dup@example.com",
		"account_id": "acct-dup",
	}); err != nil {
		t.Fatalf("UpdateCredentials(existing): %v", err)
	}

	handler.refreshToken = func(_ context.Context, refreshToken string, _ string) (*auth.TokenData, *auth.AccountInfo, error) {
		switch refreshToken {
		case "rt-dup":
			return &auth.TokenData{
				AccessToken:  "at-dup",
				RefreshToken: refreshToken,
				IDToken:      "id-dup",
				ExpiresAt:    time.Unix(1700000000, 0),
			}, &auth.AccountInfo{
				Email:            "dup@example.com",
				ChatGPTAccountID: "acct-dup",
				PlanType:         "pro",
			}, nil
		case "rt-bad":
			return nil, nil, context.DeadlineExceeded
		default:
			return &auth.TokenData{
				AccessToken:  "at-ok",
				RefreshToken: refreshToken,
				IDToken:      "id-ok",
				ExpiresAt:    time.Unix(1700000000, 0),
			}, &auth.AccountInfo{
				Email:            "ok@example.com",
				ChatGPTAccountID: "acct-ok",
				PlanType:         "team",
			}, nil
		}
	}

	body := bytes.NewBufferString("{\"name\":\"batch\",\"refresh_token\":\"rt-dup\\nrt-bad\\nrt-ok\",\"proxy_url\":\"\"}")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/accounts", body)
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.AddAccount(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := int(payload["success"].(float64)); got != 1 {
		t.Fatalf("success = %d, want %d", got, 1)
	}
	if got := int(payload["duplicate"].(float64)); got != 1 {
		t.Fatalf("duplicate = %d, want %d", got, 1)
	}
	if got := int(payload["failed"].(float64)); got != 1 {
		t.Fatalf("failed = %d, want %d", got, 1)
	}

	rows, err := handler.db.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive(): %v", err)
	}
	if got := len(rows); got != 2 {
		t.Fatalf("active accounts = %d, want %d", got, 2)
	}
}

func TestAddATAccountSkipsRequestAndDatabaseDuplicates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, cleanup := newAddAccountTestHandler(t)
	defer cleanup()

	if _, err := handler.db.InsertATAccount(context.Background(), "existing-at", "at-existing", ""); err != nil {
		t.Fatalf("InsertATAccount(existing): %v", err)
	}

	body := bytes.NewBufferString("{\"name\":\"batch-at\",\"access_token\":\"at-existing\\nat-new\\nat-new\",\"proxy_url\":\"\"}")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/accounts/at", body)
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.AddATAccount(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := int(payload["success"].(float64)); got != 1 {
		t.Fatalf("success = %d, want %d", got, 1)
	}
	if got := int(payload["duplicate"].(float64)); got != 2 {
		t.Fatalf("duplicate = %d, want %d", got, 2)
	}
	if got := int(payload["failed"].(float64)); got != 0 {
		t.Fatalf("failed = %d, want %d", got, 0)
	}

	rows, err := handler.db.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive(): %v", err)
	}
	if got := len(rows); got != 2 {
		t.Fatalf("active accounts = %d, want %d", got, 2)
	}
}

func TestAddATAccountSkipsIdentityDuplicates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, cleanup := newAddAccountTestHandler(t)
	defer cleanup()

	existingID, err := handler.db.InsertATAccount(context.Background(), "existing-at", buildAccessToken("dup@example.com", "acct-dup", "pro"), "")
	if err != nil {
		t.Fatalf("InsertATAccount(existing): %v", err)
	}
	if err := handler.db.UpdateCredentials(context.Background(), existingID, map[string]interface{}{
		"email":      "dup@example.com",
		"account_id": "acct-dup",
	}); err != nil {
		t.Fatalf("UpdateCredentials(existing): %v", err)
	}

	body := bytes.NewBufferString("{\"name\":\"batch-at\",\"access_token\":\"" + buildAccessToken("dup@example.com", "acct-dup", "pro") + "\\n" + buildAccessToken("ok@example.com", "acct-ok", "team") + "\",\"proxy_url\":\"\"}")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/accounts/at", body)
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.AddATAccount(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := int(payload["success"].(float64)); got != 1 {
		t.Fatalf("success = %d, want %d", got, 1)
	}
	if got := int(payload["duplicate"].(float64)); got != 1 {
		t.Fatalf("duplicate = %d, want %d", got, 1)
	}
	if got := int(payload["failed"].(float64)); got != 0 {
		t.Fatalf("failed = %d, want %d", got, 0)
	}

	rows, err := handler.db.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive(): %v", err)
	}
	if got := len(rows); got != 2 {
		t.Fatalf("active accounts = %d, want %d", got, 2)
	}
}

func newAddAccountTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "accounts.db")
	db, err := database.New("sqlite", dbPath)
	if err != nil {
		t.Fatalf("database.New(sqlite): %v", err)
	}

	tc := cache.NewMemory(4)
	store := auth.NewStore(db, tc, &database.SystemSettings{
		MaxConcurrency:  2,
		TestConcurrency: 1,
		TestModel:       "gpt-5.4",
	})

	handler := NewHandler(store, db, tc, nil, "")
	handler.refreshAccount = func(context.Context, int64) error { return nil }

	cleanup := func() {
		_ = tc.Close()
		_ = db.Close()
	}
	return handler, cleanup
}

func buildAccessToken(email string, accountID string, planType string) string {
	payload, _ := json.Marshal(map[string]any{
		"exp": time.Now().Add(time.Hour).Unix(),
		"jti": time.Now().UnixNano(),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
			"chatgpt_plan_type":  planType,
		},
		"https://api.openai.com/profile": map[string]any{
			"email": email,
		},
	})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
