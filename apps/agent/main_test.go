package main

import "testing"

func TestParseV2RayStatsFiltersTrackedUsers(t *testing.T) {
	stats := []v2rayStat{
		{Name: "user>>>usr_1>>>traffic>>>uplink", Value: 100},
		{Name: "user>>>usr_1>>>traffic>>>downlink", Value: 250},
		{Name: "user>>>usr_2>>>traffic>>>uplink", Value: 999},
		{Name: "inbound>>>anytls-in>>>traffic>>>uplink", Value: 888},
	}

	events := parseV2RayStats(stats, []string{"usr_1"})
	if len(events) != 1 {
		t.Fatalf("expected one tracked event, got %#v", events)
	}
	if events[0].UserID != "usr_1" || events[0].UploadBytes != 100 || events[0].DownloadBytes != 250 || events[0].Source != "v2ray-api" {
		t.Fatalf("unexpected event: %#v", events[0])
	}
}

func TestParseV2RayStatsSkipsZeroTraffic(t *testing.T) {
	events := parseV2RayStats([]v2rayStat{{Name: "user>>>usr_1>>>traffic>>>uplink", Value: 0}}, []string{"usr_1"})
	if len(events) != 0 {
		t.Fatalf("expected no zero-value event, got %#v", events)
	}
}
