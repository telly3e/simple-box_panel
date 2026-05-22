package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

CREATE TABLE IF NOT EXISTS entry_nodes (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  public_host TEXT NOT NULL,
  public_anytls_port INTEGER NOT NULL,
  public_ss_port INTEGER NOT NULL,
  exit_node_id TEXT NOT NULL REFERENCES exit_nodes(id) ON DELETE CASCADE,
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
	return err
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
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entry_nodes`).Scan(&out.EntryNodeCount); err != nil {
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
		ID:             ids.NewID("usr"),
		Name:           name,
		Enabled:        true,
		QuotaBytes:     quotaBytes,
		AnyTLSPassword: ids.NewSecret(24),
		SSPassword:     ids.NewSecret(16),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (id, name, enabled, quota_bytes, used_bytes, anytls_password, ss_password, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Name, boolInt(u.Enabled), u.QuotaBytes, u.UsedBytes, u.AnyTLSPassword, u.SSPassword,
		u.CreatedAt.Format(time.RFC3339Nano), u.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.User{}, err
	}
	_ = s.touchAllExitNodes(ctx)
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, enabled, quota_bytes, used_bytes, anytls_password, ss_password, created_at, updated_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []domain.User
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
	row := s.db.QueryRowContext(ctx, `SELECT id, name, enabled, quota_bytes, used_bytes, anytls_password, ss_password, created_at, updated_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

type UserPatch struct {
	Name       *string `json:"name"`
	Enabled    *bool   `json:"enabled"`
	QuotaBytes *int64  `json:"quota_bytes"`
}

func (s *Store) PatchUser(ctx context.Context, id string, patch UserPatch) (domain.User, error) {
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if patch.Name != nil {
		u.Name = *patch.Name
	}
	if patch.Enabled != nil {
		u.Enabled = *patch.Enabled
	}
	if patch.QuotaBytes != nil {
		if *patch.QuotaBytes < 0 {
			return domain.User{}, errors.New("quota_bytes must be non-negative")
		}
		u.QuotaBytes = *patch.QuotaBytes
	}
	u.UpdatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE users SET name = ?, enabled = ?, quota_bytes = ?, updated_at = ? WHERE id = ?`,
		u.Name, boolInt(u.Enabled), u.QuotaBytes, u.UpdatedAt.Format(time.RFC3339Nano), u.ID)
	if err != nil {
		return domain.User{}, err
	}
	_ = s.touchAllExitNodes(ctx)
	return s.GetUser(ctx, id)
}

func (s *Store) CreateExitNode(ctx context.Context, node domain.ExitNode) (domain.ExitNode, error) {
	if node.Name == "" || node.Hostname == "" {
		return domain.ExitNode{}, errors.New("name and hostname are required")
	}
	if node.AnyTLSPort == 0 {
		node.AnyTLSPort = 2443
	}
	if node.SSPort == 0 {
		node.SSPort = 8388
	}
	if node.CertMode == "" {
		node.CertMode = domain.CertModeManual
	}
	node.ID = ids.NewID("exit")
	node.ExpectedConfigVersion = 1
	node.CreatedAt = time.Now().UTC()
	node.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO exit_nodes (id, name, hostname, anytls_port, ss_port, cert_mode, cert_domain, certificate_path, key_path, acme_email, cloudflare_api_token_env, expected_config_version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.Name, node.Hostname, node.AnyTLSPort, node.SSPort, node.CertMode, node.CertDomain,
		node.CertificatePath, node.KeyPath, node.AcmeEmail, node.CloudflareAPITokenEnv,
		node.ExpectedConfigVersion, node.CreatedAt.Format(time.RFC3339Nano), node.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.ExitNode{}, err
	}
	return node, nil
}

func (s *Store) ListExitNodes(ctx context.Context) ([]domain.ExitNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, hostname, anytls_port, ss_port, cert_mode, cert_domain, certificate_path, key_path, acme_email, cloudflare_api_token_env, last_heartbeat_at, expected_config_version, created_at, updated_at FROM exit_nodes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []domain.ExitNode
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
	row := s.db.QueryRowContext(ctx, `SELECT id, name, hostname, anytls_port, ss_port, cert_mode, cert_domain, certificate_path, key_path, acme_email, cloudflare_api_token_env, last_heartbeat_at, expected_config_version, created_at, updated_at FROM exit_nodes WHERE id = ?`, id)
	return scanExitNode(row)
}

type ExitNodePatch struct {
	Name                  *string `json:"name"`
	Hostname              *string `json:"hostname"`
	AnyTLSPort            *int    `json:"anytls_port"`
	SSPort                *int    `json:"ss_port"`
	CertMode              *string `json:"cert_mode"`
	CertDomain            *string `json:"cert_domain"`
	CertificatePath       *string `json:"certificate_path"`
	KeyPath               *string `json:"key_path"`
	AcmeEmail             *string `json:"acme_email"`
	CloudflareAPITokenEnv *string `json:"cloudflare_api_token_env"`
}

func (s *Store) PatchExitNode(ctx context.Context, id string, patch ExitNodePatch) (domain.ExitNode, error) {
	n, err := s.GetExitNode(ctx, id)
	if err != nil {
		return domain.ExitNode{}, err
	}
	if patch.Name != nil {
		n.Name = *patch.Name
	}
	if patch.Hostname != nil {
		n.Hostname = *patch.Hostname
	}
	if patch.AnyTLSPort != nil {
		n.AnyTLSPort = *patch.AnyTLSPort
	}
	if patch.SSPort != nil {
		n.SSPort = *patch.SSPort
	}
	if patch.CertMode != nil {
		n.CertMode = *patch.CertMode
	}
	if patch.CertDomain != nil {
		n.CertDomain = *patch.CertDomain
	}
	if patch.CertificatePath != nil {
		n.CertificatePath = *patch.CertificatePath
	}
	if patch.KeyPath != nil {
		n.KeyPath = *patch.KeyPath
	}
	if patch.AcmeEmail != nil {
		n.AcmeEmail = *patch.AcmeEmail
	}
	if patch.CloudflareAPITokenEnv != nil {
		n.CloudflareAPITokenEnv = *patch.CloudflareAPITokenEnv
	}
	n.ExpectedConfigVersion++
	n.UpdatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
UPDATE exit_nodes SET name=?, hostname=?, anytls_port=?, ss_port=?, cert_mode=?, cert_domain=?, certificate_path=?, key_path=?, acme_email=?, cloudflare_api_token_env=?, expected_config_version=?, updated_at=? WHERE id=?`,
		n.Name, n.Hostname, n.AnyTLSPort, n.SSPort, n.CertMode, n.CertDomain, n.CertificatePath, n.KeyPath,
		n.AcmeEmail, n.CloudflareAPITokenEnv, n.ExpectedConfigVersion, n.UpdatedAt.Format(time.RFC3339Nano), n.ID)
	if err != nil {
		return domain.ExitNode{}, err
	}
	return s.GetExitNode(ctx, id)
}

func (s *Store) CreateEntryNode(ctx context.Context, node domain.EntryNode) (domain.EntryNode, error) {
	if node.Name == "" || node.PublicHost == "" || node.ExitNodeID == "" {
		return domain.EntryNode{}, errors.New("name, public_host, and exit_node_id are required")
	}
	if _, err := s.GetExitNode(ctx, node.ExitNodeID); err != nil {
		return domain.EntryNode{}, fmt.Errorf("exit node not found: %w", err)
	}
	if node.PublicAnyTLSPort == 0 {
		node.PublicAnyTLSPort = 443
	}
	if node.PublicSSPort == 0 {
		node.PublicSSPort = 8443
	}
	node.ID = ids.NewID("entry")
	node.CreatedAt = time.Now().UTC()
	node.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO entry_nodes (id, name, public_host, public_anytls_port, public_ss_port, exit_node_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.Name, node.PublicHost, node.PublicAnyTLSPort, node.PublicSSPort, node.ExitNodeID,
		node.CreatedAt.Format(time.RFC3339Nano), node.UpdatedAt.Format(time.RFC3339Nano))
	return node, err
}

func (s *Store) ListEntryNodes(ctx context.Context) ([]domain.EntryNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, public_host, public_anytls_port, public_ss_port, exit_node_id, created_at, updated_at FROM entry_nodes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []domain.EntryNode
	for rows.Next() {
		n, err := scanEntryNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (s *Store) ListEntryNodesForExit(ctx context.Context, exitNodeID string) ([]domain.EntryNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, public_host, public_anytls_port, public_ss_port, exit_node_id, created_at, updated_at FROM entry_nodes WHERE exit_node_id = ? ORDER BY created_at DESC`, exitNodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []domain.EntryNode
	for rows.Next() {
		n, err := scanEntryNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (s *Store) GetEntryNode(ctx context.Context, id string) (domain.EntryNode, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, public_host, public_anytls_port, public_ss_port, exit_node_id, created_at, updated_at FROM entry_nodes WHERE id = ?`, id)
	return scanEntryNode(row)
}

type EntryNodePatch struct {
	Name             *string `json:"name"`
	PublicHost       *string `json:"public_host"`
	PublicAnyTLSPort *int    `json:"public_anytls_port"`
	PublicSSPort     *int    `json:"public_ss_port"`
	ExitNodeID       *string `json:"exit_node_id"`
}

func (s *Store) PatchEntryNode(ctx context.Context, id string, patch EntryNodePatch) (domain.EntryNode, error) {
	n, err := s.GetEntryNode(ctx, id)
	if err != nil {
		return domain.EntryNode{}, err
	}
	if patch.Name != nil {
		n.Name = *patch.Name
	}
	if patch.PublicHost != nil {
		n.PublicHost = *patch.PublicHost
	}
	if patch.PublicAnyTLSPort != nil {
		n.PublicAnyTLSPort = *patch.PublicAnyTLSPort
	}
	if patch.PublicSSPort != nil {
		n.PublicSSPort = *patch.PublicSSPort
	}
	if patch.ExitNodeID != nil {
		if _, err := s.GetExitNode(ctx, *patch.ExitNodeID); err != nil {
			return domain.EntryNode{}, fmt.Errorf("exit node not found: %w", err)
		}
		n.ExitNodeID = *patch.ExitNodeID
	}
	n.UpdatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE entry_nodes SET name=?, public_host=?, public_anytls_port=?, public_ss_port=?, exit_node_id=?, updated_at=? WHERE id=?`,
		n.Name, n.PublicHost, n.PublicAnyTLSPort, n.PublicSSPort, n.ExitNodeID, n.UpdatedAt.Format(time.RFC3339Nano), n.ID)
	if err != nil {
		return domain.EntryNode{}, err
	}
	_ = s.touchExitNode(ctx, n.ExitNodeID)
	return s.GetEntryNode(ctx, id)
}

func (s *Store) RecordHeartbeat(ctx context.Context, nodeID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE exit_nodes SET last_heartbeat_at = ?, updated_at = ? WHERE id = ?`, nowString(), nowString(), nodeID)
	return err
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
	return s.touchAllExitNodes(ctx)
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
	err := row.Scan(&u.ID, &u.Name, &enabled, &u.QuotaBytes, &u.UsedBytes, &u.AnyTLSPassword, &u.SSPassword, &created, &updated)
	u.Enabled = enabled == 1
	u.CreatedAt = parseTime(created)
	u.UpdatedAt = parseTime(updated)
	return u, err
}

func scanExitNode(row scanner) (domain.ExitNode, error) {
	var n domain.ExitNode
	var heartbeat sql.NullString
	var created, updated string
	err := row.Scan(&n.ID, &n.Name, &n.Hostname, &n.AnyTLSPort, &n.SSPort, &n.CertMode, &n.CertDomain, &n.CertificatePath, &n.KeyPath, &n.AcmeEmail, &n.CloudflareAPITokenEnv, &heartbeat, &n.ExpectedConfigVersion, &created, &updated)
	n.LastHeartbeatAt = nullableTime(heartbeat)
	n.CreatedAt = parseTime(created)
	n.UpdatedAt = parseTime(updated)
	return n, err
}

func scanEntryNode(row scanner) (domain.EntryNode, error) {
	var n domain.EntryNode
	var created, updated string
	err := row.Scan(&n.ID, &n.Name, &n.PublicHost, &n.PublicAnyTLSPort, &n.PublicSSPort, &n.ExitNodeID, &created, &updated)
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
