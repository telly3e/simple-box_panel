package configgen

import (
	"encoding/json"

	"singpanel/internal/domain"
)

type ServerDesiredConfig struct {
	NodeID          string         `json:"node_id"`
	Version         int64          `json:"version"`
	SingBoxConfig   map[string]any `json:"sing_box_config"`
	TrackedUserIDs  []string       `json:"tracked_user_ids"`
	StatsMode       string         `json:"stats_mode"`
	StatsAPITarget  string         `json:"stats_api_target,omitempty"`
	RuntimeFileHint string         `json:"runtime_file_hint,omitempty"`
}

func GenerateServerConfig(node domain.ExitNode, users []domain.User) ServerDesiredConfig {
	tracked := make([]string, 0, len(users))
	anyTLSUsers := make([]map[string]any, 0, len(users))
	ssUsers := make([]map[string]any, 0, len(users))
	for _, user := range users {
		if !user.Active() {
			continue
		}
		tracked = append(tracked, user.ID)
		anyTLSUsers = append(anyTLSUsers, map[string]any{
			"name":     user.ID,
			"password": user.AnyTLSPassword,
		})
		ssUsers = append(ssUsers, map[string]any{
			"name":     user.ID,
			"password": user.SSPassword,
		})
	}

	anyTLSTLS := map[string]any{"enabled": true}
	providers := make([]map[string]any, 0, 1)
	if node.CertMode == domain.CertModeACME {
		anyTLSTLS["certificate_provider"] = "edge-cert"
		provider := map[string]any{
			"type":     "acme",
			"tag":      "edge-cert",
			"domain":   []string{node.CertDomain},
			"provider": "letsencrypt",
		}
		if node.AcmeEmail != "" {
			provider["email"] = node.AcmeEmail
		}
		if node.CloudflareAPITokenEnv != "" {
			provider["dns01_challenge"] = map[string]any{
				"provider":      "cloudflare",
				"api_token_env": node.CloudflareAPITokenEnv,
			}
		}
		providers = append(providers, provider)
	} else {
		if node.CertificatePath != "" {
			anyTLSTLS["certificate_path"] = node.CertificatePath
		}
		if node.KeyPath != "" {
			anyTLSTLS["key_path"] = node.KeyPath
		}
	}

	config := map[string]any{
		"log": map[string]any{"level": "info"},
		"inbounds": []map[string]any{
			{
				"type":        "anytls",
				"tag":         "anytls-in",
				"listen":      "::",
				"listen_port": node.AnyTLSPort,
				"users":       anyTLSUsers,
				"tls":         anyTLSTLS,
			},
			{
				"type":        "shadowsocks",
				"tag":         "ss-in",
				"listen":      "::",
				"listen_port": node.SSPort,
				"method":      domain.SSMethod,
				"users":       ssUsers,
			},
		},
		"outbounds": []map[string]any{
			{"type": "direct", "tag": "direct"},
		},
	}
	if len(providers) > 0 {
		config["certificate_providers"] = providers
	}

	return ServerDesiredConfig{
		NodeID:          node.ID,
		Version:         node.ExpectedConfigVersion,
		SingBoxConfig:   config,
		TrackedUserIDs:  tracked,
		StatsMode:       "mock",
		RuntimeFileHint: "sing-box.json",
	}
}

func GenerateClientSubscription(user domain.User, entries []domain.EntryNode) map[string]any {
	outbounds := []map[string]any{
		{"type": "selector", "tag": "proxy", "outbounds": []string{}},
		{"type": "direct", "tag": "direct"},
	}
	selectorTargets := []string{}
	for _, entry := range entries {
		anyTLSTag := entry.Name + " AnyTLS"
		ssTag := entry.Name + " Shadowsocks"
		selectorTargets = append(selectorTargets, anyTLSTag, ssTag)
		outbounds = append(outbounds, map[string]any{
			"type":        "anytls",
			"tag":         anyTLSTag,
			"server":      entry.PublicHost,
			"server_port": entry.PublicAnyTLSPort,
			"password":    user.AnyTLSPassword,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": entry.PublicHost,
			},
		})
		outbounds = append(outbounds, map[string]any{
			"type":        "shadowsocks",
			"tag":         ssTag,
			"server":      entry.PublicHost,
			"server_port": entry.PublicSSPort,
			"method":      domain.SSMethod,
			"password":    user.SSPassword,
		})
	}
	outbounds[0]["outbounds"] = selectorTargets
	return map[string]any{
		"log":       map[string]any{"level": "info"},
		"outbounds": outbounds,
		"route":     map[string]any{"final": "proxy"},
	}
}

func PrettyJSON(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}
