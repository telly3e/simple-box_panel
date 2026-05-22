package store

import (
	"context"
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

func TestEntryRequiresExistingExitNode(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, err := s.CreateEntryNode(ctx, domain.EntryNode{Name: "entry", PublicHost: "hk.example.com", ExitNodeID: "missing"})
	if err == nil {
		t.Fatal("expected missing exit node error")
	}
	exit, err := s.CreateExitNode(ctx, domain.ExitNode{Name: "exit", Hostname: "exit.local"})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.CreateEntryNode(ctx, domain.EntryNode{Name: "entry", PublicHost: "hk.example.com", ExitNodeID: exit.ID})
	if err != nil {
		t.Fatal(err)
	}
	if entry.PublicAnyTLSPort != 443 || entry.PublicSSPort != 8443 {
		t.Fatalf("unexpected default ports: %#v", entry)
	}
}
