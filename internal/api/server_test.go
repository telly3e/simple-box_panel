package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"singpanel/internal/configgen"
	"singpanel/internal/domain"
	"singpanel/internal/store"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewServer(s)
}

func TestAPILocalLoop(t *testing.T) {
	handler := newTestHandler(t)

	var user domain.User
	doJSON(t, handler, http.MethodPost, "/api/users", map[string]any{
		"name":        "alice",
		"quota_bytes": int64(1024 * 1024),
	}, http.StatusCreated, &user)
	if user.ID == "" || user.AnyTLSPassword == "" || user.SSPassword == "" {
		t.Fatalf("user was not initialized: %#v", user)
	}

	var exit domain.ExitNode
	doJSON(t, handler, http.MethodPost, "/api/exit-nodes", map[string]any{
		"name":             "HK Exit",
		"hostname":         "exit.local",
		"anytls_port":      2443,
		"ss_port":          8388,
		"cert_mode":        "manual",
		"certificate_path": "/etc/sing-box/cert.pem",
		"key_path":         "/etc/sing-box/key.pem",
	}, http.StatusCreated, &exit)

	var entry domain.EntryNode
	doJSON(t, handler, http.MethodPost, "/api/entry-nodes", map[string]any{
		"name":               "HK Entry",
		"public_host":        "hk.example.com",
		"public_anytls_port": 443,
		"public_ss_port":     8443,
		"exit_node_id":       exit.ID,
	}, http.StatusCreated, &entry)

	var subscription map[string]any
	doJSON(t, handler, http.MethodGet, "/api/subscriptions/"+user.ID+"/sing-box.json", nil, http.StatusOK, &subscription)
	outbounds := subscription["outbounds"].([]any)
	if len(outbounds) != 4 {
		t.Fatalf("expected selector/direct/anytls/ss outbounds, got %#v", outbounds)
	}

	var desired configgen.ServerDesiredConfig
	doJSON(t, handler, http.MethodGet, "/api/agent/"+exit.ID+"/desired-config", nil, http.StatusOK, &desired)
	if desired.NodeID != exit.ID || len(desired.TrackedUserIDs) != 1 || desired.TrackedUserIDs[0] != user.ID {
		t.Fatalf("unexpected desired config metadata: %#v", desired)
	}

	doJSON(t, handler, http.MethodPost, "/api/agent/"+exit.ID+"/traffic", map[string]any{
		"events": []map[string]any{{
			"user_id":        user.ID,
			"upload_bytes":   100,
			"download_bytes": 200,
			"source":         "test",
		}},
	}, http.StatusOK, nil)

	var summary domain.Summary
	doJSON(t, handler, http.MethodGet, "/api/summary", nil, http.StatusOK, &summary)
	if summary.TotalUsedBytes != 300 {
		t.Fatalf("expected 300 used bytes, got %d", summary.TotalUsedBytes)
	}
}

func TestSubscriptionRejectsDisabledUser(t *testing.T) {
	handler := newTestHandler(t)
	var user domain.User
	doJSON(t, handler, http.MethodPost, "/api/users", map[string]any{"name": "bob"}, http.StatusCreated, &user)
	doJSON(t, handler, http.MethodPatch, "/api/users/"+user.ID, map[string]any{"enabled": false}, http.StatusOK, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/"+user.ID+"/sing-box.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled user, got %d: %s", rec.Code, rec.Body.String())
	}
}

func doJSON(t *testing.T, handler http.Handler, method, path string, body any, wantStatus int, out any) {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &payload)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s: expected %d, got %d: %s", method, path, wantStatus, rec.Code, rec.Body.String())
	}
	if out != nil {
		if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
}
