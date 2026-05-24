package configgen

import (
	"testing"

	"singpanel/internal/domain"
)

func TestGenerateServerConfigSkipsDisabledAndOverQuotaUsers(t *testing.T) {
	node := domain.ExitNode{ID: "exit_1", Enabled: true, AnyTLSPort: 2443, SSPort: 8388, CertMode: domain.CertModeManual, ExpectedConfigVersion: 2}
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
		Enabled:               true,
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

func TestGenerateServerConfigAddsAnyTLSPaddingScheme(t *testing.T) {
	node := domain.ExitNode{
		ID:                    "exit_1",
		Enabled:               true,
		AnyTLSEnabled:         true,
		SSEnabled:             false,
		AnyTLSPort:            2443,
		CertMode:              domain.CertModeManual,
		ExpectedConfigVersion: 2,
	}
	cfg := GenerateServerConfig(node, []domain.User{{ID: "active", Enabled: true, AnyTLSPassword: "a"}})
	inbounds := cfg.SingBoxConfig["inbounds"].([]map[string]any)
	padding := inbounds[0]["padding_scheme"].([]string)
	if len(padding) != len(defaultAnyTLSPaddingScheme) || padding[0] != "stop=8" || padding[len(padding)-1] != "7=500-1000" {
		t.Fatalf("unexpected anytls padding scheme: %#v", padding)
	}
}

func TestGenerateServerConfigUsesCustomAnyTLSPaddingScheme(t *testing.T) {
	node := domain.ExitNode{
		ID:                  "exit_1",
		Enabled:             true,
		AnyTLSEnabled:       true,
		SSEnabled:           false,
		AnyTLSPort:          2443,
		AnyTLSPaddingScheme: "stop=2\n\n0=10-20\n1=30-40",
		CertMode:            domain.CertModeManual,
	}
	cfg := GenerateServerConfig(node, []domain.User{{ID: "active", Enabled: true, AnyTLSPassword: "a"}})
	inbounds := cfg.SingBoxConfig["inbounds"].([]map[string]any)
	padding := inbounds[0]["padding_scheme"].([]string)
	if len(padding) != 3 || padding[0] != "stop=2" || padding[1] != "0=10-20" || padding[2] != "1=30-40" {
		t.Fatalf("unexpected custom anytls padding scheme: %#v", padding)
	}
}

func TestGenerateServerConfigForPausedNodeHasNoInbounds(t *testing.T) {
	node := domain.ExitNode{
		ID:                    "exit_1",
		Enabled:               false,
		AnyTLSEnabled:         true,
		SSEnabled:             true,
		AnyTLSPort:            2443,
		SSPort:                8388,
		StatsMode:             domain.StatsModeV2RayAPI,
		ExpectedConfigVersion: 3,
	}
	cfg := GenerateServerConfig(node, []domain.User{{ID: "active", Enabled: true, AnyTLSPassword: "a", SSPassword: "s"}})
	if !cfg.Paused {
		t.Fatalf("expected paused desired config")
	}
	if len(cfg.TrackedUserIDs) != 0 {
		t.Fatalf("expected no tracked users while paused, got %#v", cfg.TrackedUserIDs)
	}
	inbounds := cfg.SingBoxConfig["inbounds"].([]map[string]any)
	if len(inbounds) != 0 {
		t.Fatalf("expected no inbounds while paused, got %#v", inbounds)
	}
	if _, ok := cfg.SingBoxConfig["experimental"]; ok {
		t.Fatalf("did not expect v2ray api while paused: %#v", cfg.SingBoxConfig)
	}
}

func TestGenerateServerConfigHonorsProtocolSwitches(t *testing.T) {
	node := domain.ExitNode{
		ID:                    "exit_1",
		Enabled:               true,
		AnyTLSEnabled:         false,
		SSEnabled:             true,
		AnyTLSPort:            2443,
		SSPort:                8388,
		CertMode:              domain.CertModeACME,
		CertDomain:            "example.com",
		StatsMode:             domain.StatsModeV2RayAPI,
		StatsAPIListen:        "127.0.0.1:10085",
		ExpectedConfigVersion: 2,
	}
	cfg := GenerateServerConfig(node, []domain.User{{ID: "active", Enabled: true, SSPassword: "s"}})
	inbounds := cfg.SingBoxConfig["inbounds"].([]map[string]any)
	if len(inbounds) != 1 || inbounds[0]["type"] != "shadowsocks" {
		t.Fatalf("expected only shadowsocks inbound, got %#v", inbounds)
	}
	if _, ok := cfg.SingBoxConfig["certificate_providers"]; ok {
		t.Fatalf("did not expect certificate providers when anytls is disabled: %#v", cfg.SingBoxConfig)
	}
	stats := cfg.SingBoxConfig["experimental"].(map[string]any)["v2ray_api"].(map[string]any)["stats"].(map[string]any)
	statsInbounds := stats["inbounds"].([]string)
	if len(statsInbounds) != 1 || statsInbounds[0] != "ss-in" {
		t.Fatalf("unexpected stats inbounds: %#v", statsInbounds)
	}
}

func TestGenerateServerConfigUsesCloudflareTokenEnvPlaceholder(t *testing.T) {
	node := domain.ExitNode{
		ID:                    "exit_1",
		Enabled:               true,
		AnyTLSPort:            2443,
		SSPort:                8388,
		CertMode:              domain.CertModeACME,
		CertDomain:            "example.com",
		CloudflareAPITokenEnv: "CF_TOKEN",
		ExpectedConfigVersion: 2,
	}
	cfg := GenerateServerConfig(node, nil)
	providers := cfg.SingBoxConfig["certificate_providers"].([]map[string]any)
	challenge := providers[0]["dns01_challenge"].(map[string]any)
	if challenge["api_token_env"] != "CF_TOKEN" {
		t.Fatalf("unexpected dns01 placeholder: %#v", challenge)
	}
}

func TestGenerateClientSubscriptionUsesNodeHostAndUserPasswords(t *testing.T) {
	user := domain.User{ID: "usr_1", Enabled: true, AnyTLSPassword: "any-pass", SSPassword: "ss-pass"}
	nodes := []domain.ExitNode{{Name: "HK", Hostname: "hk.example.com", Enabled: true, AnyTLSEnabled: true, SSEnabled: true, AnyTLSPort: 443, SSPort: 8443, SSMethod: domain.SSMethodAES128GCM}}
	cfg := GenerateClientSubscription(user, nodes)
	outbounds := cfg["outbounds"].([]map[string]any)
	anytls := outbounds[2]
	if anytls["server"] != "hk.example.com" || anytls["password"] != "any-pass" {
		t.Fatalf("unexpected anytls outbound: %#v", anytls)
	}
	ss := outbounds[3]
	if ss["method"] != domain.SSMethodAES128GCM || ss["password"] != "ss-pass" {
		t.Fatalf("unexpected ss outbound: %#v", ss)
	}
}

func TestGenerateShadowsocks2022ConfigUsesGeneratedKeysAndMultiplex(t *testing.T) {
	node := domain.ExitNode{
		ID:                     "exit_1",
		Name:                   "HK",
		Hostname:               "hk.example.com",
		Enabled:                true,
		AnyTLSEnabled:          false,
		SSEnabled:              true,
		SSPort:                 8443,
		SSMethod:               domain.SSMethod2022Blake3AES256GCM,
		SS2022ServerPassword32: "server-key-32",
		ExpectedConfigVersion:  2,
	}
	user := domain.User{ID: "usr_1", Enabled: true, SSPassword: "legacy", SS2022Password32: "user-key-32"}

	server := GenerateServerConfig(node, []domain.User{user})
	inbound := server.SingBoxConfig["inbounds"].([]map[string]any)[0]
	if inbound["method"] != domain.SSMethod2022Blake3AES256GCM || inbound["password"] != "server-key-32" {
		t.Fatalf("unexpected 2022 inbound method/password: %#v", inbound)
	}
	multiplex := inbound["multiplex"].(map[string]any)
	if multiplex["enabled"] != true {
		t.Fatalf("expected inbound multiplex enabled: %#v", inbound)
	}
	users := inbound["users"].([]map[string]any)
	if users[0]["password"] != "user-key-32" {
		t.Fatalf("expected 2022 user key, got %#v", users[0])
	}

	subscription := GenerateClientSubscription(user, []domain.ExitNode{node})
	ss := subscription["outbounds"].([]map[string]any)[2]
	if ss["password"] != "server-key-32:user-key-32" {
		t.Fatalf("expected 2022 client password to combine server/user keys, got %#v", ss)
	}
	if ss["multiplex"].(map[string]any)["enabled"] != true {
		t.Fatalf("expected client multiplex enabled: %#v", ss)
	}
}

func TestGenerateClientSubscriptionUsesRelayAndProtocolSwitches(t *testing.T) {
	user := domain.User{ID: "usr_1", Enabled: true, AnyTLSPassword: "any-pass", SSPassword: "ss-pass"}
	nodes := []domain.ExitNode{{
		Name:            "JP",
		Hostname:        "origin.example.com",
		Enabled:         true,
		AnyTLSEnabled:   false,
		SSEnabled:       true,
		SSPort:          8388,
		RelayEnabled:    true,
		RelayHost:       "relay.example.com",
		RelayAnyTLSPort: 443,
		RelaySSPort:     18443,
	}}
	cfg := GenerateClientSubscription(user, nodes)
	outbounds := cfg["outbounds"].([]map[string]any)
	if len(outbounds) != 3 {
		t.Fatalf("expected selector/direct/ss only, got %#v", outbounds)
	}
	ss := outbounds[2]
	if ss["server"] != "relay.example.com" || ss["server_port"] != 18443 {
		t.Fatalf("unexpected relay ss outbound: %#v", ss)
	}
}

func TestGenerateClientSubscriptionSkipsPausedNodes(t *testing.T) {
	user := domain.User{ID: "usr_1", Enabled: true, AnyTLSPassword: "any-pass", SSPassword: "ss-pass"}
	nodes := []domain.ExitNode{{
		Name:          "Paused",
		Hostname:      "paused.example.com",
		Enabled:       false,
		AnyTLSEnabled: true,
		SSEnabled:     true,
		AnyTLSPort:    443,
		SSPort:        8443,
	}}
	cfg := GenerateClientSubscription(user, nodes)
	outbounds := cfg["outbounds"].([]map[string]any)
	if len(outbounds) != 2 {
		t.Fatalf("expected selector/direct only, got %#v", outbounds)
	}
	targets := outbounds[0]["outbounds"].([]string)
	if len(targets) != 0 {
		t.Fatalf("expected no selector targets, got %#v", targets)
	}
}
