package configgen

import (
	"encoding/json"
	"strings"

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
	Paused          bool           `json:"paused"`
}

var defaultAnyTLSPaddingScheme = []string{
	"stop=8",
	"0=30-30",
	"1=100-400",
	"2=400-500,c,500-1000,c,500-1000,c,500-1000,c,500-1000",
	"3=9-9,500-1000",
	"4=500-1000",
	"5=500-1000",
	"6=500-1000",
	"7=500-1000",
}

func GenerateServerConfig(node domain.ExitNode, users []domain.User) ServerDesiredConfig {
	if !node.Enabled {
		return ServerDesiredConfig{
			NodeID:          node.ID,
			Version:         node.ExpectedConfigVersion,
			SingBoxConfig:   pausedSingBoxConfig(),
			TrackedUserIDs:  []string{},
			StatsMode:       statsModeOrDefault(node),
			RuntimeFileHint: "sing-box.json",
			Paused:          true,
		}
	}

	anyTLSEnabled, ssEnabled := enabledProtocols(node)
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
			"password": user.SSPasswordForMethod(node.SSMethod),
		})
	}

	inbounds := make([]map[string]any, 0, 2)
	providers := make([]map[string]any, 0, 1)
	if anyTLSEnabled {
		anyTLSTLS := map[string]any{"enabled": true}
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
		inbounds = append(inbounds, map[string]any{
			"type":           "anytls",
			"tag":            "anytls-in",
			"listen":         "::",
			"listen_port":    node.AnyTLSPort,
			"users":          anyTLSUsers,
			"padding_scheme": anyTLSPaddingScheme(node.AnyTLSPaddingScheme),
			"tls":            anyTLSTLS,
		})
	}
	if ssEnabled {
		method := domain.SSMethodOrDefault(node.SSMethod)
		inbound := map[string]any{
			"type":        "shadowsocks",
			"tag":         "ss-in",
			"listen":      "::",
			"listen_port": node.SSPort,
			"method":      method,
			"password":    node.SSServerPasswordForMethod(method),
			"users":       ssUsers,
			"multiplex":   map[string]any{"enabled": true},
		}
		inbounds = append(inbounds, inbound)
	}

	config := map[string]any{
		"log":      map[string]any{"level": "info"},
		"inbounds": inbounds,
		"outbounds": []map[string]any{
			{"type": "direct", "tag": "direct"},
		},
	}
	if len(providers) > 0 {
		config["certificate_providers"] = providers
	}

	statsMode := statsModeOrDefault(node)
	statsAPITarget := ""
	if statsMode == domain.StatsModeV2RayAPI {
		statsAPITarget = node.StatsAPIListen
		if statsAPITarget == "" {
			statsAPITarget = "127.0.0.1:10085"
		}
		statsInbounds := []string{}
		if anyTLSEnabled {
			statsInbounds = append(statsInbounds, "anytls-in")
		}
		if ssEnabled {
			statsInbounds = append(statsInbounds, "ss-in")
		}
		config["experimental"] = map[string]any{
			"v2ray_api": map[string]any{
				"listen": statsAPITarget,
				"stats": map[string]any{
					"enabled":   true,
					"inbounds":  statsInbounds,
					"outbounds": []string{"direct"},
					"users":     tracked,
				},
			},
		}
	}

	return ServerDesiredConfig{
		NodeID:          node.ID,
		Version:         node.ExpectedConfigVersion,
		SingBoxConfig:   config,
		TrackedUserIDs:  tracked,
		StatsMode:       statsMode,
		StatsAPITarget:  statsAPITarget,
		RuntimeFileHint: "sing-box.json",
	}
}

func GenerateClientSubscription(user domain.User, nodes []domain.ExitNode) map[string]any {
	outbounds := []map[string]any{
		{"type": "selector", "tag": "proxy", "outbounds": []string{}},
		{"type": "direct", "tag": "direct"},
	}
	selectorTargets := []string{}
	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		anyTLSEnabled, ssEnabled := enabledProtocols(node)
		host := node.SubscriptionHost()
		if host == "" {
			continue
		}
		if anyTLSEnabled {
			anyTLSTag := node.Name + " AnyTLS"
			selectorTargets = append(selectorTargets, anyTLSTag)
			outbounds = append(outbounds, map[string]any{
				"type":        "anytls",
				"tag":         anyTLSTag,
				"server":      host,
				"server_port": node.SubscriptionAnyTLSPort(),
				"password":    user.AnyTLSPassword,
				"tls": map[string]any{
					"enabled":     true,
					"server_name": node.Hostname,
				},
			})
		}
		if ssEnabled {
			method := domain.SSMethodOrDefault(node.SSMethod)
			password := user.SSPasswordForMethod(method)
			if domain.IsSS2022Method(method) {
				password = node.SSServerPasswordForMethod(method) + ":" + password
			}
			ssTag := node.Name + " Shadowsocks"
			selectorTargets = append(selectorTargets, ssTag)
			outbounds = append(outbounds, map[string]any{
				"type":        "shadowsocks",
				"tag":         ssTag,
				"server":      host,
				"server_port": node.SubscriptionSSPort(),
				"method":      method,
				"password":    password,
				"multiplex":   map[string]any{"enabled": true},
			})
		}
	}
	outbounds[0]["outbounds"] = selectorTargets
	return map[string]any{
		"log":       map[string]any{"level": "info"},
		"outbounds": outbounds,
		"route":     map[string]any{"final": "proxy"},
	}
}

func enabledProtocols(node domain.ExitNode) (bool, bool) {
	if !node.AnyTLSEnabled && !node.SSEnabled {
		return true, true
	}
	return node.AnyTLSEnabled, node.SSEnabled
}

func statsModeOrDefault(node domain.ExitNode) string {
	if node.StatsMode == "" {
		return domain.StatsModeMock
	}
	return node.StatsMode
}

func anyTLSPaddingScheme(custom string) []string {
	lines := []string{}
	for _, line := range strings.Split(custom, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 {
		return lines
	}
	return append([]string(nil), defaultAnyTLSPaddingScheme...)
}

func pausedSingBoxConfig() map[string]any {
	return map[string]any{
		"log":      map[string]any{"level": "info"},
		"inbounds": []map[string]any{},
		"outbounds": []map[string]any{
			{"type": "direct", "tag": "direct"},
		},
	}
}

func PrettyJSON(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}
