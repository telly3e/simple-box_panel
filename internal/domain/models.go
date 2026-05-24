package domain

import "time"

const (
	CertModeManual = "manual"
	CertModeACME   = "acme"

	StatsModeMock     = "mock"
	StatsModeV2RayAPI = "v2ray-api"

	SSMethod2022Blake3AES128GCM        = "2022-blake3-aes-128-gcm"
	SSMethod2022Blake3AES256GCM        = "2022-blake3-aes-256-gcm"
	SSMethod2022Blake3Chacha20Poly1305 = "2022-blake3-chacha20-poly1305"
	SSMethodNone                       = "none"
	SSMethodAES128GCM                  = "aes-128-gcm"
	SSMethodAES192GCM                  = "aes-192-gcm"
	SSMethodAES256GCM                  = "aes-256-gcm"
	SSMethodChacha20IETFPoly1305       = "chacha20-ietf-poly1305"
	SSMethodXChacha20IETFPoly1305      = "xchacha20-ietf-poly1305"

	SSDefaultMethod = SSMethodAES128GCM
)

var SupportedSSMethods = []string{
	SSMethod2022Blake3AES128GCM,
	SSMethod2022Blake3AES256GCM,
	SSMethod2022Blake3Chacha20Poly1305,
	SSMethodNone,
	SSMethodAES128GCM,
	SSMethodAES192GCM,
	SSMethodAES256GCM,
	SSMethodChacha20IETFPoly1305,
	SSMethodXChacha20IETFPoly1305,
}

type User struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Enabled           bool      `json:"enabled"`
	QuotaBytes        int64     `json:"quota_bytes"`
	UsedBytes         int64     `json:"used_bytes"`
	AnyTLSPassword    string    `json:"anytls_password"`
	SSPassword        string    `json:"ss_password"`
	SS2022Password16  string    `json:"ss_2022_password_16"`
	SS2022Password32  string    `json:"ss_2022_password_32"`
	SubscriptionToken string    `json:"subscription_token"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (u User) Active() bool {
	return u.Enabled && (u.QuotaBytes == 0 || u.UsedBytes < u.QuotaBytes)
}

type ExitNode struct {
	ID                     string     `json:"id"`
	Name                   string     `json:"name"`
	Hostname               string     `json:"hostname"`
	Enabled                bool       `json:"enabled"`
	AnyTLSEnabled          bool       `json:"anytls_enabled"`
	SSEnabled              bool       `json:"ss_enabled"`
	AnyTLSPort             int        `json:"anytls_port"`
	AnyTLSPaddingScheme    string     `json:"anytls_padding_scheme"`
	SSPort                 int        `json:"ss_port"`
	SSMethod               string     `json:"ss_method"`
	SS2022ServerPassword16 string     `json:"ss_2022_server_password_16"`
	SS2022ServerPassword32 string     `json:"ss_2022_server_password_32"`
	RelayEnabled           bool       `json:"relay_enabled"`
	RelayHost              string     `json:"relay_host"`
	RelayAnyTLSPort        int        `json:"relay_anytls_port"`
	RelaySSPort            int        `json:"relay_ss_port"`
	CertMode               string     `json:"cert_mode"`
	CertDomain             string     `json:"cert_domain"`
	CertificatePath        string     `json:"certificate_path"`
	KeyPath                string     `json:"key_path"`
	AcmeEmail              string     `json:"acme_email"`
	CloudflareAPITokenEnv  string     `json:"cloudflare_api_token_env"`
	AgentToken             string     `json:"agent_token"`
	StatsMode              string     `json:"stats_mode"`
	StatsAPIListen         string     `json:"stats_api_listen"`
	LastHeartbeatAt        *time.Time `json:"last_heartbeat_at,omitempty"`
	AppliedConfigVersion   int64      `json:"applied_config_version"`
	LastAppliedAt          *time.Time `json:"last_applied_at,omitempty"`
	LastAgentError         string     `json:"last_agent_error"`
	LastAgentErrorAt       *time.Time `json:"last_agent_error_at,omitempty"`
	ExpectedConfigVersion  int64      `json:"expected_config_version"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func (n ExitNode) SubscriptionHost() string {
	if n.RelayEnabled && n.RelayHost != "" {
		return n.RelayHost
	}
	return n.Hostname
}

func (n ExitNode) SubscriptionAnyTLSPort() int {
	if n.RelayEnabled && n.RelayAnyTLSPort != 0 {
		return n.RelayAnyTLSPort
	}
	return n.AnyTLSPort
}

func (n ExitNode) SubscriptionSSPort() int {
	if n.RelayEnabled && n.RelaySSPort != 0 {
		return n.RelaySSPort
	}
	return n.SSPort
}

func ValidSSMethod(method string) bool {
	if method == "" {
		return true
	}
	for _, supported := range SupportedSSMethods {
		if method == supported {
			return true
		}
	}
	return false
}

func SSMethodOrDefault(method string) string {
	if method == "" {
		return SSDefaultMethod
	}
	return method
}

func SS2022KeyLength(method string) int {
	switch method {
	case SSMethod2022Blake3AES128GCM:
		return 16
	case SSMethod2022Blake3AES256GCM, SSMethod2022Blake3Chacha20Poly1305:
		return 32
	default:
		return 0
	}
}

func IsSS2022Method(method string) bool {
	return SS2022KeyLength(method) > 0
}

func (u User) SSPasswordForMethod(method string) string {
	switch SS2022KeyLength(method) {
	case 16:
		if u.SS2022Password16 != "" {
			return u.SS2022Password16
		}
	case 32:
		if u.SS2022Password32 != "" {
			return u.SS2022Password32
		}
	}
	return u.SSPassword
}

func (n ExitNode) SSServerPasswordForMethod(method string) string {
	switch SS2022KeyLength(method) {
	case 16:
		if n.SS2022ServerPassword16 != "" {
			return n.SS2022ServerPassword16
		}
	case 32:
		if n.SS2022ServerPassword32 != "" {
			return n.SS2022ServerPassword32
		}
	}
	if n.SS2022ServerPassword16 != "" {
		return n.SS2022ServerPassword16
	}
	return n.SS2022ServerPassword32
}

type TrafficEvent struct {
	ID            string    `json:"id"`
	NodeID        string    `json:"node_id"`
	UserID        string    `json:"user_id"`
	UploadBytes   int64     `json:"upload_bytes"`
	DownloadBytes int64     `json:"download_bytes"`
	Source        string    `json:"source"`
	CreatedAt     time.Time `json:"created_at"`
}

type Summary struct {
	UserCount       int   `json:"user_count"`
	EnabledUsers    int   `json:"enabled_users"`
	ExitNodeCount   int   `json:"exit_node_count"`
	TotalUsedBytes  int64 `json:"total_used_bytes"`
	OnlineExitNodes int   `json:"online_exit_nodes"`
}
