package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseV2RayStatsFiltersTrackedUsers(t *testing.T) {
	stats := []v2rayStat{
		{Name: "user>>>usr_1>>>traffic>>>uplink", Value: 100},
		{Name: "user>>>usr_1>>>traffic>>>downlink", Value: 250},
		{Name: "user>>>usr_2>>>traffic>>>uplink", Value: 999},
		{Name: "inbound>>>anytls-in>>>traffic>>>uplink", Value: 888},
	}

	events := parseV2RayStats(stats, []string{"usr_1"})
	if len(events) != 1 {
		t.Fatalf("expected one tracked event, got %#v", events)
	}
	if events[0].UserID != "usr_1" || events[0].UploadBytes != 100 || events[0].DownloadBytes != 250 || events[0].Source != "v2ray-api" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
}

func TestParseV2RayStatsSkipsZeroTraffic(t *testing.T) {
	events := parseV2RayStats([]v2rayStat{{Name: "user>>>usr_1>>>traffic>>>uplink", Value: 0}}, []string{"usr_1"})
	if len(events) != 0 {
		t.Fatalf("expected no zero-value event, got %#v", events)
	}
}

func TestResolveEnvPlaceholders(t *testing.T) {
	t.Setenv("CF_TOKEN", "secret-token")
	cfg := map[string]any{
		"certificate_providers": []map[string]any{
			{
				"type": "acme",
				"dns01_challenge": map[string]any{
					"provider":      "cloudflare",
					"api_token_env": "CF_TOKEN",
				},
			},
		},
	}
	if err := resolveEnvPlaceholders(cfg); err != nil {
		t.Fatalf("resolve env placeholders: %v", err)
	}
	providers := cfg["certificate_providers"].([]map[string]any)
	challenge := providers[0]["dns01_challenge"].(map[string]any)
	if challenge["api_token"] != "secret-token" {
		t.Fatalf("expected resolved api_token, got %#v", challenge)
	}
	if _, ok := challenge["api_token_env"]; ok {
		t.Fatalf("api_token_env should be removed: %#v", challenge)
	}
}

func TestResolveEnvPlaceholdersRequiresValue(t *testing.T) {
	cfg := map[string]any{"api_token_env": "MISSING_TOKEN"}
	if err := resolveEnvPlaceholders(cfg); err == nil {
		t.Fatal("expected missing environment variable error")
	}
}

func TestAPIClientAddsBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Sing-Panel-Agent-Token") != "agent-secret" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client := apiClient{base: server.Client(), basicUser: "admin", basicPass: "secret", agentToken: "agent-secret"}
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestPostHeartbeatSendsAgentStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/exit_1/heartbeat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var status heartbeatStatus
		if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
			t.Fatalf("decode heartbeat: %v", err)
		}
		if status.AppliedConfigVersion != 7 || status.LastError != "reload failed" {
			t.Fatalf("unexpected heartbeat status: %#v", status)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := apiClient{base: server.Client()}
	err := postHeartbeat(client, server.URL, "exit_1", heartbeatStatus{AppliedConfigVersion: 7, LastError: "reload failed"})
	if err != nil {
		t.Fatal(err)
	}
}
