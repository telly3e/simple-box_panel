package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	if user.ID == "" || user.AnyTLSPassword == "" || user.SSPassword == "" || user.SubscriptionToken == "" {
		t.Fatalf("user was not initialized: %#v", user)
	}

	var exit domain.ExitNode
	doJSON(t, handler, http.MethodPost, "/api/exit-nodes", map[string]any{
		"name":              "HK Exit",
		"hostname":          "exit.local",
		"anytls_enabled":    true,
		"ss_enabled":        true,
		"anytls_port":       2443,
		"ss_port":           8388,
		"relay_enabled":     true,
		"relay_host":        "hk.example.com",
		"relay_anytls_port": 443,
		"relay_ss_port":     8443,
		"cert_mode":         "manual",
		"certificate_path":  "/etc/sing-box/cert.pem",
		"key_path":          "/etc/sing-box/key.pem",
	}, http.StatusCreated, &exit)
	if exit.AgentToken == "" {
		t.Fatalf("exit node was not initialized with agent token: %#v", exit)
	}

	var subscription map[string]any
	doJSON(t, handler, http.MethodGet, "/sub/"+user.SubscriptionToken, nil, http.StatusOK, &subscription)
	outbounds := subscription["outbounds"].([]any)
	if len(outbounds) != 4 {
		t.Fatalf("expected selector/direct/anytls/ss outbounds, got %#v", outbounds)
	}

	var desired configgen.ServerDesiredConfig
	doJSONWithAgentToken(t, handler, http.MethodGet, "/api/agent/"+exit.ID+"/desired-config", exit.AgentToken, nil, http.StatusOK, &desired)
	if desired.NodeID != exit.ID || len(desired.TrackedUserIDs) != 1 || desired.TrackedUserIDs[0] != user.ID {
		t.Fatalf("unexpected desired config metadata: %#v", desired)
	}

	doJSONWithAgentToken(t, handler, http.MethodPost, "/api/agent/"+exit.ID+"/heartbeat", exit.AgentToken, map[string]any{
		"applied_config_version": desired.Version,
	}, http.StatusOK, nil)

	doJSONWithAgentToken(t, handler, http.MethodPost, "/api/agent/"+exit.ID+"/traffic", exit.AgentToken, map[string]any{
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

	var resetUser domain.User
	doJSON(t, handler, http.MethodPatch, "/api/users/"+user.ID, map[string]any{"reset_used_bytes": true}, http.StatusOK, &resetUser)
	if resetUser.UsedBytes != 0 {
		t.Fatalf("expected reset user traffic, got %d", resetUser.UsedBytes)
	}
	doJSON(t, handler, http.MethodDelete, "/api/users/"+user.ID, nil, http.StatusNoContent, nil)
	doJSON(t, handler, http.MethodGet, "/sub/"+user.SubscriptionToken, nil, http.StatusNotFound, nil)
}

func TestAgentEndpointsRequireToken(t *testing.T) {
	handler := newTestHandler(t)
	var node domain.ExitNode
	doJSON(t, handler, http.MethodPost, "/api/exit-nodes", map[string]any{
		"name":     "exit",
		"hostname": "exit.local",
	}, http.StatusCreated, &node)

	for _, token := range []string{"", "wrong-token"} {
		req := httptest.NewRequest(http.MethodGet, "/api/agent/"+node.ID+"/desired-config", nil)
		if token != "" {
			req.Header.Set("X-Sing-Panel-Agent-Token", token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for token %q, got %d: %s", token, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agent/"+node.ID+"/desired-config", nil)
	req.Header.Set("Authorization", "Bearer "+node.AgentToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected bearer token to be accepted, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPausedExitNodeReturnsPausedDesiredConfig(t *testing.T) {
	handler := newTestHandler(t)
	var user domain.User
	doJSON(t, handler, http.MethodPost, "/api/users", map[string]any{"name": "bob"}, http.StatusCreated, &user)
	var node domain.ExitNode
	doJSON(t, handler, http.MethodPost, "/api/exit-nodes", map[string]any{
		"name":     "exit",
		"hostname": "exit.local",
	}, http.StatusCreated, &node)
	doJSON(t, handler, http.MethodPatch, "/api/exit-nodes/"+node.ID, map[string]any{"enabled": false}, http.StatusOK, &node)

	var desired configgen.ServerDesiredConfig
	doJSONWithAgentToken(t, handler, http.MethodGet, "/api/agent/"+node.ID+"/desired-config", node.AgentToken, nil, http.StatusOK, &desired)
	if !desired.Paused || len(desired.TrackedUserIDs) != 0 {
		t.Fatalf("expected paused desired config with no tracked users, got %#v", desired)
	}
	inbounds := desired.SingBoxConfig["inbounds"].([]any)
	if len(inbounds) != 0 {
		t.Fatalf("expected paused desired config to have no inbounds, got %#v", inbounds)
	}

	var subscription map[string]any
	doJSON(t, handler, http.MethodGet, "/sub/"+user.SubscriptionToken, nil, http.StatusOK, &subscription)
	outbounds := subscription["outbounds"].([]any)
	if len(outbounds) != 2 {
		t.Fatalf("expected paused node to be skipped in subscription, got %#v", outbounds)
	}
}

func TestDeleteExitNodeRemovesItFromManagement(t *testing.T) {
	handler := newTestHandler(t)
	var node domain.ExitNode
	doJSON(t, handler, http.MethodPost, "/api/exit-nodes", map[string]any{
		"name":     "exit",
		"hostname": "exit.local",
	}, http.StatusCreated, &node)
	doJSON(t, handler, http.MethodDelete, "/api/exit-nodes/"+node.ID, nil, http.StatusNoContent, nil)
	doJSONWithAgentToken(t, handler, http.MethodGet, "/api/agent/"+node.ID+"/desired-config", node.AgentToken, nil, http.StatusNotFound, nil)
}

func TestSubscriptionRejectsDisabledUser(t *testing.T) {
	handler := newTestHandler(t)
	var user domain.User
	doJSON(t, handler, http.MethodPost, "/api/users", map[string]any{"name": "bob"}, http.StatusCreated, &user)
	doJSON(t, handler, http.MethodPatch, "/api/users/"+user.ID, map[string]any{"enabled": false}, http.StatusOK, nil)

	req := httptest.NewRequest(http.MethodGet, "/sub/"+user.SubscriptionToken, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled user, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOldSubscriptionPathIsRemoved(t *testing.T) {
	handler := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/usr_123/sing-box.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected old subscription path to return 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListEndpointsReturnEmptyArrays(t *testing.T) {
	handler := newTestHandler(t)
	for _, path := range []string{"/api/users", "/api/exit-nodes"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
		if strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatalf("%s: expected empty array, got %s", path, rec.Body.String())
		}
	}
}

func TestServerServesWebDistWithSPAFallback(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<div id="root">panel</div>`), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.Mkdir(filepath.Join(webDir, "assets"), 0o755); err != nil {
		t.Fatalf("make assets dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte(`console.log("ok")`), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	handler := NewServerWithWeb(s, webDir)
	for _, path := range []string{"/", "/subscriptions"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "panel") {
			t.Fatalf("expected index for %s, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("expected asset, got %d: %s", rec.Code, rec.Body.String())
	}
}

func doJSON(t *testing.T, handler http.Handler, method, path string, body any, wantStatus int, out any) {
	t.Helper()
	doJSONWithHeaders(t, handler, method, path, nil, body, wantStatus, out)
}

func doJSONWithAgentToken(t *testing.T, handler http.Handler, method, path, token string, body any, wantStatus int, out any) {
	t.Helper()
	doJSONWithHeaders(t, handler, method, path, map[string]string{"X-Sing-Panel-Agent-Token": token}, body, wantStatus, out)
}

func doJSONWithHeaders(t *testing.T, handler http.Handler, method, path string, headers map[string]string, body any, wantStatus int, out any) {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &payload)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
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
