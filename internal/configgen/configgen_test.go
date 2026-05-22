package configgen

import (
	"testing"

	"singpanel/internal/domain"
)

func TestGenerateServerConfigSkipsDisabledAndOverQuotaUsers(t *testing.T) {
	node := domain.ExitNode{ID: "exit_1", AnyTLSPort: 2443, SSPort: 8388, CertMode: domain.CertModeManual, ExpectedConfigVersion: 2}
	users := []domain.User{
		{ID: "active", Enabled: true, AnyTLSPassword: "a", SSPassword: "s", QuotaBytes: 100, UsedBytes: 10},
		{ID: "disabled", Enabled: false, AnyTLSPassword: "b", SSPassword: "s", QuotaBytes: 100},
		{ID: "over", Enabled: true, AnyTLSPassword: "c", SSPassword: "s", QuotaBytes: 1, UsedBytes: 1},
	}
	cfg := GenerateServerConfig(node, users)
	if len(cfg.TrackedUserIDs) != 1 || cfg.TrackedUserIDs[0] != "active" {
		t.Fatalf("expected only active user, got %#v", cfg.TrackedUserIDs)
	}
}

func TestGenerateServerConfigAddsV2RayAPIStatsWhenEnabled(t *testing.T) {
	node := domain.ExitNode{
		ID:                    "exit_1",
		AnyTLSPort:            2443,
		SSPort:                8388,
		CertMode:              domain.CertModeManual,
		StatsMode:             domain.StatsModeV2RayAPI,
		StatsAPIListen:        "127.0.0.1:10085",
		ExpectedConfigVersion: 2,
	}
	users := []domain.User{{ID: "active", Enabled: true, AnyTLSPassword: "a", SSPassword: "s"}}
	cfg := GenerateServerConfig(node, users)
	if cfg.StatsMode != domain.StatsModeV2RayAPI || cfg.StatsAPITarget != "127.0.0.1:10085" {
		t.Fatalf("unexpected stats metadata: %#v", cfg)
	}
	experimental := cfg.SingBoxConfig["experimental"].(map[string]any)
	v2rayAPI := experimental["v2ray_api"].(map[string]any)
	if v2rayAPI["listen"] != "127.0.0.1:10085" {
		t.Fatalf("unexpected v2ray api listen: %#v", v2rayAPI)
	}
	stats := v2rayAPI["stats"].(map[string]any)
	usersList := stats["users"].([]string)
	if len(usersList) != 1 || usersList[0] != "active" {
		t.Fatalf("unexpected stats users: %#v", usersList)
	}
}

func TestGenerateClientSubscriptionUsesEntryHostAndUserPasswords(t *testing.T) {
	user := domain.User{ID: "usr_1", Enabled: true, AnyTLSPassword: "any-pass", SSPassword: "ss-pass"}
	entries := []domain.EntryNode{{Name: "HK", PublicHost: "hk.example.com", PublicAnyTLSPort: 443, PublicSSPort: 8443}}
	cfg := GenerateClientSubscription(user, entries)
	outbounds := cfg["outbounds"].([]map[string]any)
	anytls := outbounds[2]
	if anytls["server"] != "hk.example.com" || anytls["password"] != "any-pass" {
		t.Fatalf("unexpected anytls outbound: %#v", anytls)
	}
	ss := outbounds[3]
	if ss["method"] != domain.SSMethod || ss["password"] != "ss-pass" {
		t.Fatalf("unexpected ss outbound: %#v", ss)
	}
}
