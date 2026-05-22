package domain

import "time"

const (
	CertModeManual = "manual"
	CertModeACME   = "acme"

	StatsModeMock     = "mock"
	StatsModeV2RayAPI = "v2ray-api"

	SSMethod = "aes-128-gcm"
)

type User struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Enabled        bool      `json:"enabled"`
	QuotaBytes     int64     `json:"quota_bytes"`
	UsedBytes      int64     `json:"used_bytes"`
	AnyTLSPassword string    `json:"anytls_password"`
	SSPassword     string    `json:"ss_password"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (u User) Active() bool {
	return u.Enabled && (u.QuotaBytes == 0 || u.UsedBytes < u.QuotaBytes)
}

type ExitNode struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	Hostname              string     `json:"hostname"`
	AnyTLSPort            int        `json:"anytls_port"`
	SSPort                int        `json:"ss_port"`
	CertMode              string     `json:"cert_mode"`
	CertDomain            string     `json:"cert_domain"`
	CertificatePath       string     `json:"certificate_path"`
	KeyPath               string     `json:"key_path"`
	AcmeEmail             string     `json:"acme_email"`
	CloudflareAPITokenEnv string     `json:"cloudflare_api_token_env"`
	StatsMode             string     `json:"stats_mode"`
	StatsAPIListen        string     `json:"stats_api_listen"`
	LastHeartbeatAt       *time.Time `json:"last_heartbeat_at,omitempty"`
	ExpectedConfigVersion int64      `json:"expected_config_version"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type EntryNode struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	PublicHost       string    `json:"public_host"`
	PublicAnyTLSPort int       `json:"public_anytls_port"`
	PublicSSPort     int       `json:"public_ss_port"`
	ExitNodeID       string    `json:"exit_node_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
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
	EntryNodeCount  int   `json:"entry_node_count"`
	TotalUsedBytes  int64 `json:"total_used_bytes"`
	OnlineExitNodes int   `json:"online_exit_nodes"`
}
