package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"singpanel/internal/domain"
	"singpanel/internal/ids"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL,
  quota_bytes INTEGER NOT NULL,
  used_bytes INTEGER NOT NULL,
  anytls_password TEXT NOT NULL,
  ss_password TEXT NOT NULL,
  subscription_token TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS exit_nodes (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  hostname TEXT NOT NULL,
  anytls_port INTEGER NOT NULL,
  ss_port INTEGER NOT NULL,
  cert_mode TEXT NOT NULL,
  cert_domain TEXT NOT NULL,
  certificate_path TEXT NOT NULL,
  key_path TEXT NOT NULL,
  acme_email TEXT NOT NULL,
  cloudflare_api_token_env TEXT NOT NULL,
  last_heartbeat_at TEXT,
  expected_config_version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS traffic_events (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES exit_nodes(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  upload_bytes INTEGER NOT NULL,
  download_bytes INTEGER NOT NULL,
  source TEXT NOT NULL,
  created_at TEXT NOT NULL
);
`)
	if err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "users", "subscription_token", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "users", "ss_2022_password_16", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "users", "ss_2022_password_32", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.backfillSubscriptionTokens(ctx); err != nil {
		return err
	}
	if err := s.backfillUserSS2022Keys(ctx); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "stats_mode", "TEXT NOT NULL DEFAULT 'mock'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "stats_api_listen", "TEXT NOT NULL DEFAULT '127.0.0.1:10085'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "agent_token", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.backfillAgentTokens(ctx); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "enabled", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "anytls_enabled", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "anytls_padding_scheme", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "ss_enabled", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "ss_method", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "ss_2022_server_password_16", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "ss_2022_server_password_32", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.backfillExitNodeSSFields(ctx); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "relay_enabled", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "relay_host", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "relay_anytls_port", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "relay_ss_port", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "applied_config_version", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "last_applied_at", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "exit_nodes", "last_agent_error", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return s.ensureColumn(ctx, "exit_nodes", "last_agent_error_at", "TEXT")
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
	return err
}

func (s *Store) backfillSubscriptionTokens(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM users WHERE subscription_token = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	idsToUpdate := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		idsToUpdate = append(idsToUpdate, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range idsToUpdate {
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET subscription_token = ? WHERE id = ?`, ids.NewToken(32), id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backfillUserSS2022Keys(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, ss_2022_password_16, ss_2022_password_32 FROM users WHERE ss_2022_password_16 = '' OR ss_2022_password_32 = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type userKeys struct {
		id    string
		key16 string
		key32 string
	}
	updates := []userKeys{}
	for rows.Next() {
		var item userKeys
		if err := rows.Scan(&item.id, &item.key16, &item.key32); err != nil {
			return err
		}
		updates = append(updates, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range updates {
		if item.key16 == "" {
			item.key16 = ids.NewSecret(16)
		}
		if item.key32 == "" {
			item.key32 = ids.NewSecret(32)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET ss_2022_password_16 = ?, ss_2022_password_32 = ? WHERE id = ?`, item.key16, item.key32, item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backfillAgentTokens(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM exit_nodes WHERE agent_token = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	idsToUpdate := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		idsToUpdate = append(idsToUpdate, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range idsToUpdate {
		if _, err := s.db.ExecContext(ctx, `UPDATE exit_nodes SET agent_token = ? WHERE id = ?`, ids.NewToken(32), id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backfillExitNodeSSFields(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, ss_method, ss_2022_server_password_16, ss_2022_server_password_32 FROM exit_nodes WHERE ss_method = '' OR ss_2022_server_password_16 = '' OR ss_2022_server_password_32 = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type nodeKeys struct {
		id     string
		method string
		key16  string
		key32  string
	}
	updates := []nodeKeys{}
	for rows.Next() {
		var item nodeKeys
		if err := rows.Scan(&item.id, &item.method, &item.key16, &item.key32); err != nil {
			return err
		}
		updates = append(updates, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range updates {
		if item.method == "" {
			item.method = domain.SSDefaultMethod
		}
		if item.key16 == "" {
			item.key16 = ids.NewSecret(16)
		}
		if item.key32 == "" {
			item.key32 = ids.NewSecret(32)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE exit_nodes SET ss_method = ?, ss_2022_server_password_16 = ?, ss_2022_server_password_32 = ? WHERE id = ?`, item.method, item.key16, item.key32, item.id); err != nil {
			return err
		}
	}
	return nil
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

func nullableTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	t := parseTime(value.String)
	return &t
}

func (s *Store) Summary(ctx context.Context) (domain.Summary, error) {
	var out domain.Summary
	err := s.db.QueryRowContext(ctx, `
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN enabled = 1 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(used_bytes), 0)
FROM users`).Scan(&out.UserCount, &out.EnabledUsers, &out.TotalUsedBytes)
	if err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM exit_nodes`).Scan(&out.ExitNodeCount); err != nil {
		return out, err
	}
	cutoff := time.Now().UTC().Add(-90 * time.Second).Format(time.RFC3339Nano)
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM exit_nodes WHERE last_heartbeat_at IS NOT NULL AND last_heartbeat_at >= ?`, cutoff).Scan(&out.OnlineExitNodes)
	return out, err
}

func (s *Store) CreateUser(ctx context.Context, name string, quotaBytes int64) (domain.User, error) {
	if name == "" {
		return domain.User{}, errors.New("name is required")
	}
	if quotaBytes < 0 {
		return domain.User{}, errors.New("quota_bytes must be non-negative")
	}
	u := domain.User{
		ID:                ids.NewID("usr"),
		Name:              name,
		Enabled:           true,
		QuotaBytes:        quotaBytes,
		AnyTLSPassword:    ids.NewSecret(24),
		SSPassword:        ids.NewSecret(16),
		SS2022Password16:  ids.NewSecret(16),
		SS2022Password32:  ids.NewSecret(32),
		SubscriptionToken: ids.NewToken(32),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (id, name, enabled, quota_bytes, used_bytes, anytls_password, ss_password, ss_2022_password_16, ss_2022_password_32, subscription_token, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Name, boolInt(u.Enabled), u.QuotaBytes, u.UsedBytes, u.AnyTLSPassword, u.SSPassword, u.SS2022Password16, u.SS2022Password32,
		u.SubscriptionToken,
		u.CreatedAt.Format(time.RFC3339Nano), u.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.User{}, err
	}
	_ = s.touchAllExitNodes(ctx)
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, enabled, quota_bytes, used_bytes, anytls_password, ss_password, ss_2022_password_16, ss_2022_password_32, subscription_token, created_at, updated_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []domain.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) ActiveUsers(ctx context.Context) ([]domain.User, error) {
	users, err := s.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	active := make([]domain.User, 0, len(users))
	for _, u := range users {
		if u.Active() {
			active = append(active, u)
		}
	}
	return active, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, enabled, quota_bytes, used_bytes, anytls_password, ss_password, ss_2022_password_16, ss_2022_password_32, subscription_token, created_at, updated_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func (s *Store) GetUserBySubscriptionToken(ctx context.Context, token string) (domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, enabled, quota_bytes, used_bytes, anytls_password, ss_password, ss_2022_password_16, ss_2022_password_32, subscription_token, created_at, updated_at FROM users WHERE subscription_token = ?`, token)
	return scanUser(row)
}

type UserPatch struct {
	Name                   *string `json:"name"`
	Enabled                *bool   `json:"enabled"`
	QuotaBytes             *int64  `json:"quota_bytes"`
	ResetSubscriptionToken *bool   `json:"reset_subscription_token"`
	ResetUsedBytes         *bool   `json:"reset_used_bytes"`
}

func (s *Store) PatchUser(ctx context.Context, id string, patch UserPatch) (domain.User, error) {
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	configChanged := false
	if patch.Name != nil {
		u.Name = *patch.Name
	}
	if patch.Enabled != nil {
		u.Enabled = *patch.Enabled
		configChanged = true
	}
	if patch.QuotaBytes != nil {
		if *patch.QuotaBytes < 0 {
			return domain.User{}, errors.New("quota_bytes must be non-negative")
		}
		u.QuotaBytes = *patch.QuotaBytes
		configChanged = true
	}
	if patch.ResetSubscriptionToken != nil && *patch.ResetSubscriptionToken {
		u.SubscriptionToken = ids.NewToken(32)
	}
	if patch.ResetUsedBytes != nil && *patch.ResetUsedBytes {
		u.UsedBytes = 0
		configChanged = true
	}
	u.UpdatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE users SET name = ?, enabled = ?, quota_bytes = ?, used_bytes = ?, subscription_token = ?, updated_at = ? WHERE id = ?`,
		u.Name, boolInt(u.Enabled), u.QuotaBytes, u.UsedBytes, u.SubscriptionToken, u.UpdatedAt.Format(time.RFC3339Nano), u.ID)
	if err != nil {
		return domain.User{}, err
	}
	if configChanged {
		_ = s.touchAllExitNodes(ctx)
	}
	return s.GetUser(ctx, id)
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return s.touchAllExitNodes(ctx)
}

func (s *Store) CreateExitNode(ctx context.Context, node domain.ExitNode) (domain.ExitNode, error) {
	if node.Name == "" || node.Hostname == "" {
		return domain.ExitNode{}, errors.New("name and hostname are required")
	}
	if !node.Enabled {
		node.Enabled = true
	}
	if !node.AnyTLSEnabled && !node.SSEnabled {
		node.AnyTLSEnabled = true
		node.SSEnabled = true
	}
	if !node.AnyTLSEnabled && !node.SSEnabled {
		return domain.ExitNode{}, errors.New("at least one protocol must be enabled")
	}
	if node.AnyTLSPort == 0 {
		node.AnyTLSPort = 2443
	}
	if node.SSPort == 0 {
		node.SSPort = 8388
	}
	node.SSMethod = domain.SSMethodOrDefault(node.SSMethod)
	if !domain.ValidSSMethod(node.SSMethod) {
		return domain.ExitNode{}, errors.New("unsupported ss_method")
	}
	if node.SS2022ServerPassword16 == "" {
		node.SS2022ServerPassword16 = ids.NewSecret(16)
	}
	if node.SS2022ServerPassword32 == "" {
		node.SS2022ServerPassword32 = ids.NewSecret(32)
	}
	if node.RelayEnabled && node.RelayHost == "" {
		return domain.ExitNode{}, errors.New("relay_host is required when relay is enabled")
	}
	if node.CertMode == "" {
		node.CertMode = domain.CertModeManual
	}
	if node.StatsMode == "" {
		node.StatsMode = domain.StatsModeMock
	}
	if node.StatsAPIListen == "" {
		node.StatsAPIListen = "127.0.0.1:10085"
	}
	if node.StatsMode != domain.StatsModeMock && node.StatsMode != domain.StatsModeV2RayAPI {
		return domain.ExitNode{}, errors.New("stats_mode must be mock or v2ray-api")
	}
	node.ID = ids.NewID("exit")
	node.AgentToken = ids.NewToken(32)
	node.ExpectedConfigVersion = 1
	node.CreatedAt = time.Now().UTC()
	node.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO exit_nodes (id, name, hostname, enabled, anytls_enabled, ss_enabled, anytls_port, anytls_padding_scheme, ss_port, ss_method, ss_2022_server_password_16, ss_2022_server_password_32, relay_enabled, relay_host, relay_anytls_port, relay_ss_port, cert_mode, cert_domain, certificate_path, key_path, acme_email, cloudflare_api_token_env, agent_token, stats_mode, stats_api_listen, expected_config_version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.Name, node.Hostname, boolInt(node.Enabled), boolInt(node.AnyTLSEnabled), boolInt(node.SSEnabled), node.AnyTLSPort, node.AnyTLSPaddingScheme, node.SSPort, node.SSMethod, node.SS2022ServerPassword16, node.SS2022ServerPassword32,
		boolInt(node.RelayEnabled), node.RelayHost, node.RelayAnyTLSPort, node.RelaySSPort, node.CertMode, node.CertDomain,
		node.CertificatePath, node.KeyPath, node.AcmeEmail, node.CloudflareAPITokenEnv,
		node.AgentToken, node.StatsMode, node.StatsAPIListen,
		node.ExpectedConfigVersion, node.CreatedAt.Format(time.RFC3339Nano), node.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.ExitNode{}, err
	}
	return node, nil
}

func (s *Store) ListExitNodes(ctx context.Context) ([]domain.ExitNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, hostname, enabled, anytls_enabled, ss_enabled, anytls_port, anytls_padding_scheme, ss_port, ss_method, ss_2022_server_password_16, ss_2022_server_password_32, relay_enabled, relay_host, relay_anytls_port, relay_ss_port, cert_mode, cert_domain, certificate_path, key_path, acme_email, cloudflare_api_token_env, agent_token, stats_mode, stats_api_listen, last_heartbeat_at, applied_config_version, last_applied_at, last_agent_error, last_agent_error_at, expected_config_version, created_at, updated_at FROM exit_nodes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []domain.ExitNode{}
	for rows.Next() {
		n, err := scanExitNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (s *Store) GetExitNode(ctx context.Context, id string) (domain.ExitNode, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, hostname, enabled, anytls_enabled, ss_enabled, anytls_port, anytls_padding_scheme, ss_port, ss_method, ss_2022_server_password_16, ss_2022_server_password_32, relay_enabled, relay_host, relay_anytls_port, relay_ss_port, cert_mode, cert_domain, certificate_path, key_path, acme_email, cloudflare_api_token_env, agent_token, stats_mode, stats_api_listen, last_heartbeat_at, applied_config_version, last_applied_at, last_agent_error, last_agent_error_at, expected_config_version, created_at, updated_at FROM exit_nodes WHERE id = ?`, id)
	return scanExitNode(row)
}

func (s *Store) DeleteExitNode(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM exit_nodes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type ExitNodePatch struct {
	Name                  *string `json:"name"`
	Hostname              *string `json:"hostname"`
	Enabled               *bool   `json:"enabled"`
	AnyTLSEnabled         *bool   `json:"anytls_enabled"`
	SSEnabled             *bool   `json:"ss_enabled"`
	AnyTLSPort            *int    `json:"anytls_port"`
	AnyTLSPaddingScheme   *string `json:"anytls_padding_scheme"`
	SSPort                *int    `json:"ss_port"`
	SSMethod              *string `json:"ss_method"`
	RelayEnabled          *bool   `json:"relay_enabled"`
	RelayHost             *string `json:"relay_host"`
	RelayAnyTLSPort       *int    `json:"relay_anytls_port"`
	RelaySSPort           *int    `json:"relay_ss_port"`
	CertMode              *string `json:"cert_mode"`
	CertDomain            *string `json:"cert_domain"`
	CertificatePath       *string `json:"certificate_path"`
	KeyPath               *string `json:"key_path"`
	AcmeEmail             *string `json:"acme_email"`
	CloudflareAPITokenEnv *string `json:"cloudflare_api_token_env"`
	ResetAgentToken       *bool   `json:"reset_agent_token"`
	StatsMode             *string `json:"stats_mode"`
	StatsAPIListen        *string `json:"stats_api_listen"`
}

func (s *Store) PatchExitNode(ctx context.Context, id string, patch ExitNodePatch) (domain.ExitNode, error) {
	n, err := s.GetExitNode(ctx, id)
	if err != nil {
		return domain.ExitNode{}, err
	}
	configChanged := false
	if patch.Name != nil {
		n.Name = *patch.Name
		configChanged = true
	}
	if patch.Hostname != nil {
		n.Hostname = *patch.Hostname
		configChanged = true
	}
	if patch.Enabled != nil {
		n.Enabled = *patch.Enabled
		configChanged = true
	}
	if patch.AnyTLSEnabled != nil {
		n.AnyTLSEnabled = *patch.AnyTLSEnabled
		configChanged = true
	}
	if patch.SSEnabled != nil {
		n.SSEnabled = *patch.SSEnabled
		configChanged = true
	}
	if patch.AnyTLSPort != nil {
		n.AnyTLSPort = *patch.AnyTLSPort
		configChanged = true
	}
	if patch.AnyTLSPaddingScheme != nil {
		n.AnyTLSPaddingScheme = *patch.AnyTLSPaddingScheme
		configChanged = true
	}
	if patch.SSPort != nil {
		n.SSPort = *patch.SSPort
		configChanged = true
	}
	if patch.SSMethod != nil {
		if !domain.ValidSSMethod(*patch.SSMethod) {
			return domain.ExitNode{}, errors.New("unsupported ss_method")
		}
		n.SSMethod = domain.SSMethodOrDefault(*patch.SSMethod)
		configChanged = true
	}
	if patch.RelayEnabled != nil {
		n.RelayEnabled = *patch.RelayEnabled
		configChanged = true
	}
	if patch.RelayHost != nil {
		n.RelayHost = *patch.RelayHost
		configChanged = true
	}
	if patch.RelayAnyTLSPort != nil {
		n.RelayAnyTLSPort = *patch.RelayAnyTLSPort
		configChanged = true
	}
	if patch.RelaySSPort != nil {
		n.RelaySSPort = *patch.RelaySSPort
		configChanged = true
	}
	if patch.CertMode != nil {
		n.CertMode = *patch.CertMode
		configChanged = true
	}
	if patch.CertDomain != nil {
		n.CertDomain = *patch.CertDomain
		configChanged = true
	}
	if patch.CertificatePath != nil {
		n.CertificatePath = *patch.CertificatePath
		configChanged = true
	}
	if patch.KeyPath != nil {
		n.KeyPath = *patch.KeyPath
		configChanged = true
	}
	if patch.AcmeEmail != nil {
		n.AcmeEmail = *patch.AcmeEmail
		configChanged = true
	}
	if patch.CloudflareAPITokenEnv != nil {
		n.CloudflareAPITokenEnv = *patch.CloudflareAPITokenEnv
		configChanged = true
	}
	if patch.ResetAgentToken != nil && *patch.ResetAgentToken {
		n.AgentToken = ids.NewToken(32)
	}
	if patch.StatsMode != nil {
		if *patch.StatsMode != domain.StatsModeMock && *patch.StatsMode != domain.StatsModeV2RayAPI {
			return domain.ExitNode{}, errors.New("stats_mode must be mock or v2ray-api")
		}
		n.StatsMode = *patch.StatsMode
		configChanged = true
	}
	if patch.StatsAPIListen != nil {
		n.StatsAPIListen = *patch.StatsAPIListen
		configChanged = true
	}
	if n.StatsAPIListen == "" {
		n.StatsAPIListen = "127.0.0.1:10085"
	}
	n.SSMethod = domain.SSMethodOrDefault(n.SSMethod)
	if n.SS2022ServerPassword16 == "" {
		n.SS2022ServerPassword16 = ids.NewSecret(16)
	}
	if n.SS2022ServerPassword32 == "" {
		n.SS2022ServerPassword32 = ids.NewSecret(32)
	}
	if !n.AnyTLSEnabled && !n.SSEnabled {
		return domain.ExitNode{}, errors.New("at least one protocol must be enabled")
	}
	if n.RelayEnabled && n.RelayHost == "" {
		return domain.ExitNode{}, errors.New("relay_host is required when relay is enabled")
	}
	if configChanged {
		n.ExpectedConfigVersion++
	}
	n.UpdatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
UPDATE exit_nodes SET name=?, hostname=?, enabled=?, anytls_enabled=?, ss_enabled=?, anytls_port=?, anytls_padding_scheme=?, ss_port=?, ss_method=?, ss_2022_server_password_16=?, ss_2022_server_password_32=?, relay_enabled=?, relay_host=?, relay_anytls_port=?, relay_ss_port=?, cert_mode=?, cert_domain=?, certificate_path=?, key_path=?, acme_email=?, cloudflare_api_token_env=?, agent_token=?, stats_mode=?, stats_api_listen=?, expected_config_version=?, updated_at=? WHERE id=?`,
		n.Name, n.Hostname, boolInt(n.Enabled), boolInt(n.AnyTLSEnabled), boolInt(n.SSEnabled), n.AnyTLSPort, n.AnyTLSPaddingScheme, n.SSPort, n.SSMethod, n.SS2022ServerPassword16, n.SS2022ServerPassword32,
		boolInt(n.RelayEnabled), n.RelayHost, n.RelayAnyTLSPort, n.RelaySSPort, n.CertMode, n.CertDomain, n.CertificatePath, n.KeyPath,
		n.AcmeEmail, n.CloudflareAPITokenEnv, n.AgentToken, n.StatsMode, n.StatsAPIListen, n.ExpectedConfigVersion, n.UpdatedAt.Format(time.RFC3339Nano), n.ID)
	if err != nil {
		return domain.ExitNode{}, err
	}
	return s.GetExitNode(ctx, id)
}

type HeartbeatInput struct {
	AppliedConfigVersion int64  `json:"applied_config_version"`
	LastError            string `json:"last_error"`
}

func (s *Store) RecordHeartbeat(ctx context.Context, nodeID string, input HeartbeatInput) error {
	ts := nowString()
	result, err := s.db.ExecContext(ctx, `
UPDATE exit_nodes SET
  last_heartbeat_at = ?,
  applied_config_version = CASE WHEN ? > 0 THEN ? ELSE applied_config_version END,
  last_applied_at = CASE WHEN ? > 0 AND ? != applied_config_version THEN ? ELSE last_applied_at END,
  last_agent_error = ?,
  last_agent_error_at = CASE WHEN ? != '' THEN ? ELSE last_agent_error_at END,
  updated_at = ?
WHERE id = ?`,
		ts,
		input.AppliedConfigVersion, input.AppliedConfigVersion,
		input.AppliedConfigVersion, input.AppliedConfigVersion, ts,
		input.LastError,
		input.LastError, ts,
		ts,
		nodeID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type TrafficInput struct {
	UserID        string `json:"user_id"`
	UploadBytes   int64  `json:"upload_bytes"`
	DownloadBytes int64  `json:"download_bytes"`
	Source        string `json:"source"`
}

func (s *Store) RecordTraffic(ctx context.Context, nodeID string, events []TrafficInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := nowString()
	for _, event := range events {
		if event.UserID == "" || event.UploadBytes < 0 || event.DownloadBytes < 0 {
			return errors.New("invalid traffic event")
		}
		source := event.Source
		if source == "" {
			source = "unknown"
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO traffic_events (id, node_id, user_id, upload_bytes, download_bytes, source, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
			ids.NewID("te"), nodeID, event.UserID, event.UploadBytes, event.DownloadBytes, source, ts)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE users SET used_bytes = used_bytes + ?, updated_at = ? WHERE id = ?`,
			event.UploadBytes+event.DownloadBytes, ts, event.UserID)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) touchAllExitNodes(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE exit_nodes SET expected_config_version = expected_config_version + 1, updated_at = ?`, nowString())
	return err
}

func (s *Store) touchExitNode(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE exit_nodes SET expected_config_version = expected_config_version + 1, updated_at = ? WHERE id = ?`, nowString(), id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (domain.User, error) {
	var u domain.User
	var enabled int
	var created, updated string
	err := row.Scan(&u.ID, &u.Name, &enabled, &u.QuotaBytes, &u.UsedBytes, &u.AnyTLSPassword, &u.SSPassword, &u.SS2022Password16, &u.SS2022Password32, &u.SubscriptionToken, &created, &updated)
	u.Enabled = enabled == 1
	u.CreatedAt = parseTime(created)
	u.UpdatedAt = parseTime(updated)
	return u, err
}

func scanExitNode(row scanner) (domain.ExitNode, error) {
	var n domain.ExitNode
	var enabled, anyTLSEnabled, ssEnabled, relayEnabled int
	var heartbeat, appliedAt, agentErrorAt sql.NullString
	var created, updated string
	err := row.Scan(&n.ID, &n.Name, &n.Hostname, &enabled, &anyTLSEnabled, &ssEnabled, &n.AnyTLSPort, &n.AnyTLSPaddingScheme, &n.SSPort, &n.SSMethod, &n.SS2022ServerPassword16, &n.SS2022ServerPassword32, &relayEnabled, &n.RelayHost, &n.RelayAnyTLSPort, &n.RelaySSPort, &n.CertMode, &n.CertDomain, &n.CertificatePath, &n.KeyPath, &n.AcmeEmail, &n.CloudflareAPITokenEnv, &n.AgentToken, &n.StatsMode, &n.StatsAPIListen, &heartbeat, &n.AppliedConfigVersion, &appliedAt, &n.LastAgentError, &agentErrorAt, &n.ExpectedConfigVersion, &created, &updated)
	n.Enabled = enabled == 1
	n.AnyTLSEnabled = anyTLSEnabled == 1
	n.SSEnabled = ssEnabled == 1
	n.RelayEnabled = relayEnabled == 1
	n.SSMethod = domain.SSMethodOrDefault(n.SSMethod)
	if n.StatsMode == "" {
		n.StatsMode = domain.StatsModeMock
	}
	if n.StatsAPIListen == "" {
		n.StatsAPIListen = "127.0.0.1:10085"
	}
	n.LastHeartbeatAt = nullableTime(heartbeat)
	n.LastAppliedAt = nullableTime(appliedAt)
	n.LastAgentErrorAt = nullableTime(agentErrorAt)
	n.CreatedAt = parseTime(created)
	n.UpdatedAt = parseTime(updated)
	return n, err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
