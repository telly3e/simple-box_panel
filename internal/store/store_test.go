package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"singpanel/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestTrafficAccruesToUser(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	user, err := s.CreateUser(ctx, "alice", 1000)
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	err = s.RecordTraffic(ctx, node.ID, []TrafficInput{{
		UserID:        user.ID,
		UploadBytes:   100,
		DownloadBytes: 250,
		Source:        "test",
	}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.UsedBytes != 350 {
		t.Fatalf("expected 350 used bytes, got %d", updated.UsedBytes)
	}
}

func TestUserSubscriptionTokenCreatedAndReset(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	user, err := s.CreateUser(ctx, "alice", 1000)
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	if user.SubscriptionToken == "" {
		t.Fatal("expected subscription token")
	}
	byToken, err := s.GetUserBySubscriptionToken(ctx, user.SubscriptionToken)
	if err != nil {
		t.Fatal(err)
	}
	if byToken.ID != user.ID {
		t.Fatalf("expected token lookup to return %s, got %s", user.ID, byToken.ID)
	}
	reset := true
	updated, err := s.PatchUser(ctx, user.ID, UserPatch{ResetSubscriptionToken: &reset})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SubscriptionToken == "" || updated.SubscriptionToken == user.SubscriptionToken {
		t.Fatalf("expected reset subscription token, got %#v", updated)
	}
	updatedNode, err := s.GetExitNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedNode.ExpectedConfigVersion != node.ExpectedConfigVersion {
		t.Fatalf("resetting subscription token should not bump config version: before=%d after=%d", node.ExpectedConfigVersion, updatedNode.ExpectedConfigVersion)
	}
}

func TestTrafficDoesNotBumpConfigVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	user, err := s.CreateUser(ctx, "alice", 1000)
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTraffic(ctx, node.ID, []TrafficInput{{UserID: user.ID, UploadBytes: 100}}); err != nil {
		t.Fatal(err)
	}
	updated, err := s.GetExitNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ExpectedConfigVersion != node.ExpectedConfigVersion {
		t.Fatalf("traffic should not bump config version: before=%d after=%d", node.ExpectedConfigVersion, updated.ExpectedConfigVersion)
	}
}

func TestResetUserTrafficBumpsConfigVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	user, err := s.CreateUser(ctx, "alice", 1000)
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTraffic(ctx, node.ID, []TrafficInput{{UserID: user.ID, UploadBytes: 100, DownloadBytes: 200}}); err != nil {
		t.Fatal(err)
	}
	reset := true
	updated, err := s.PatchUser(ctx, user.ID, UserPatch{ResetUsedBytes: &reset})
	if err != nil {
		t.Fatal(err)
	}
	if updated.UsedBytes != 0 {
		t.Fatalf("expected used bytes reset to 0, got %d", updated.UsedBytes)
	}
	updatedNode, err := s.GetExitNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedNode.ExpectedConfigVersion <= node.ExpectedConfigVersion {
		t.Fatalf("resetting traffic should bump config version: before=%d after=%d", node.ExpectedConfigVersion, updatedNode.ExpectedConfigVersion)
	}
}

func TestDeleteUserCascadesTrafficAndBumpsConfigVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	user, err := s.CreateUser(ctx, "alice", 1000)
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTraffic(ctx, node.ID, []TrafficInput{{UserID: user.ID, UploadBytes: 100}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetUser(ctx, user.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected deleted user lookup to return sql.ErrNoRows, got %v", err)
	}
	var eventCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM traffic_events WHERE user_id = ?`, user.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("expected traffic events to cascade delete, got %d", eventCount)
	}
	updatedNode, err := s.GetExitNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedNode.ExpectedConfigVersion <= node.ExpectedConfigVersion {
		t.Fatalf("deleting user should bump config version: before=%d after=%d", node.ExpectedConfigVersion, updatedNode.ExpectedConfigVersion)
	}
}

func TestExitNodeStatsModeCanSwitchToV2RayAPI(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	exit, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	if exit.AgentToken == "" {
		t.Fatal("expected agent token")
	}
	if exit.StatsMode != domain.StatsModeMock || exit.StatsAPIListen != "127.0.0.1:10085" {
		t.Fatalf("unexpected default stats fields: %#v", exit)
	}
	mode := domain.StatsModeV2RayAPI
	listen := "127.0.0.1:19090"
	updated, err := s.PatchExitNode(ctx, exit.ID, ExitNodePatch{StatsMode: &mode, StatsAPIListen: &listen})
	if err != nil {
		t.Fatal(err)
	}
	if updated.StatsMode != domain.StatsModeV2RayAPI || updated.StatsAPIListen != listen {
		t.Fatalf("unexpected patched stats fields: %#v", updated)
	}
}

func TestShadowsocksMethodAnd2022KeysAreInitialized(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	user, err := s.CreateUser(ctx, "alice", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if user.SS2022Password16 == "" || user.SS2022Password32 == "" {
		t.Fatalf("expected user 2022 keys to be initialized: %#v", user)
	}

	method := domain.SSMethod2022Blake3Chacha20Poly1305
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local", SSMethod: method})
	if err != nil {
		t.Fatal(err)
	}
	if node.SSMethod != method || node.SS2022ServerPassword16 == "" || node.SS2022ServerPassword32 == "" {
		t.Fatalf("expected node ss method and 2022 keys: %#v", node)
	}

	legacyMethod := domain.SSMethodAES256GCM
	updated, err := s.PatchExitNode(ctx, node.ID, ExitNodePatch{SSMethod: &legacyMethod})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SSMethod != legacyMethod || updated.ExpectedConfigVersion <= node.ExpectedConfigVersion {
		t.Fatalf("expected patched ss method to bump config version: before=%#v after=%#v", node, updated)
	}

	badMethod := "rc4-md5"
	if _, err := s.PatchExitNode(ctx, node.ID, ExitNodePatch{SSMethod: &badMethod}); err == nil {
		t.Fatal("expected unsupported ss_method to be rejected")
	}
}

func TestExitNodeAgentTokenCanResetWithoutBumpingConfigVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	reset := true
	updated, err := s.PatchExitNode(ctx, node.ID, ExitNodePatch{ResetAgentToken: &reset})
	if err != nil {
		t.Fatal(err)
	}
	if updated.AgentToken == "" || updated.AgentToken == node.AgentToken {
		t.Fatalf("expected reset agent token: before=%q after=%q", node.AgentToken, updated.AgentToken)
	}
	if updated.ExpectedConfigVersion != node.ExpectedConfigVersion {
		t.Fatalf("resetting agent token should not bump config version: before=%d after=%d", node.ExpectedConfigVersion, updated.ExpectedConfigVersion)
	}
}

func TestExitNodeCanBePausedAndResumed(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	if !node.Enabled {
		t.Fatalf("expected new node to be enabled: %#v", node)
	}
	enabled := false
	paused, err := s.PatchExitNode(ctx, node.ID, ExitNodePatch{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if paused.Enabled {
		t.Fatalf("expected node to be paused: %#v", paused)
	}
	if paused.ExpectedConfigVersion <= node.ExpectedConfigVersion {
		t.Fatalf("pausing node should bump config version: before=%d after=%d", node.ExpectedConfigVersion, paused.ExpectedConfigVersion)
	}
	enabled = true
	resumed, err := s.PatchExitNode(ctx, node.ID, ExitNodePatch{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Enabled {
		t.Fatalf("expected node to resume: %#v", resumed)
	}
	if resumed.ExpectedConfigVersion <= paused.ExpectedConfigVersion {
		t.Fatalf("resuming node should bump config version: before=%d after=%d", paused.ExpectedConfigVersion, resumed.ExpectedConfigVersion)
	}
}

func TestDeleteExitNodeCascadesTraffic(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	user, err := s.CreateUser(ctx, "alice", 1000)
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTraffic(ctx, node.ID, []TrafficInput{{UserID: user.ID, UploadBytes: 100}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteExitNode(ctx, node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetExitNode(ctx, node.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected deleted exit lookup to return sql.ErrNoRows, got %v", err)
	}
	var eventCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM traffic_events WHERE node_id = ?`, node.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("expected traffic events to cascade delete, got %d", eventCount)
	}
}

func TestHeartbeatStoresAgentStatus(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHeartbeat(ctx, node.ID, HeartbeatInput{AppliedConfigVersion: 3, LastError: "reload failed"}); err != nil {
		t.Fatal(err)
	}
	updated, err := s.GetExitNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastHeartbeatAt == nil || updated.LastAppliedAt == nil || updated.LastAgentErrorAt == nil {
		t.Fatalf("expected heartbeat timestamps to be set: %#v", updated)
	}
	if updated.AppliedConfigVersion != 3 || updated.LastAgentError != "reload failed" {
		t.Fatalf("unexpected heartbeat status: %#v", updated)
	}
	if err := s.RecordHeartbeat(ctx, node.ID, HeartbeatInput{AppliedConfigVersion: 3}); err != nil {
		t.Fatal(err)
	}
	updated, err = s.GetExitNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AppliedConfigVersion != 3 || updated.LastAgentError != "" {
		t.Fatalf("expected heartbeat to clear last error while preserving applied version: %#v", updated)
	}
}

func TestExitNodeRelayAndProtocolSwitches(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	node, err := s.CreateExitNode(ctx, domain.ExitNode{
		Name:            "exit",
		Hostname:        "origin.example.com",
		AnyTLSEnabled:   false,
		SSEnabled:       true,
		RelayEnabled:    true,
		RelayHost:       "relay.example.com",
		RelayAnyTLSPort: 443,
		RelaySSPort:     18443,
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.AnyTLSEnabled || !node.SSEnabled || !node.RelayEnabled {
		t.Fatalf("unexpected protocol/relay fields: %#v", node)
	}
	if node.SubscriptionHost() != "relay.example.com" || node.SubscriptionSSPort() != 18443 {
		t.Fatalf("unexpected subscription endpoint: %#v", node)
	}
	anytls := true
	ss := false
	relay := false
	updated, err := s.PatchExitNode(ctx, node.ID, ExitNodePatch{AnyTLSEnabled: &anytls, SSEnabled: &ss, RelayEnabled: &relay})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.AnyTLSEnabled || updated.SSEnabled || updated.RelayEnabled {
		t.Fatalf("unexpected patched fields: %#v", updated)
	}
}

func TestExitNodeAnyTLSPaddingSchemeCanBePatched(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	node, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	padding := "stop=2\n0=10-20\n1=30-40"
	updated, err := s.PatchExitNode(ctx, node.ID, ExitNodePatch{AnyTLSPaddingScheme: &padding})
	if err != nil {
		t.Fatal(err)
	}
	if updated.AnyTLSPaddingScheme != padding {
		t.Fatalf("unexpected padding scheme: %#v", updated)
	}
	if updated.ExpectedConfigVersion <= node.ExpectedConfigVersion {
		t.Fatalf("changing padding scheme should bump config version: before=%d after=%d", node.ExpectedConfigVersion, updated.ExpectedConfigVersion)
	}
}
