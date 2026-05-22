package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	legacyproto "github.com/golang/protobuf/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type desiredConfig struct {
	NodeID         string         `json:"node_id"`
	Version        int64          `json:"version"`
	SingBoxConfig  map[string]any `json:"sing_box_config"`
	TrackedUserIDs []string       `json:"tracked_user_ids"`
	StatsMode      string         `json:"stats_mode"`
	StatsAPITarget string         `json:"stats_api_target"`
}

type trafficEvent struct {
	UserID        string `json:"user_id"`
	UploadBytes   int64  `json:"upload_bytes"`
	DownloadBytes int64  `json:"download_bytes"`
	Source        string `json:"source"`
}

type collector interface {
	Collect(ctx context.Context, cfg desiredConfig) ([]trafficEvent, error)
}

type mockCollector struct{}

type v2rayAPICollector struct {
	reset bool
}

type v2rayStat struct {
	Name  string
	Value int64
}

type queryStatsRequest struct {
	Pattern string `protobuf:"bytes,1,opt,name=pattern,proto3" json:"pattern,omitempty"`
	Reset_  bool   `protobuf:"varint,2,opt,name=reset,proto3" json:"reset,omitempty"`
}

func (m *queryStatsRequest) Reset()         { *m = queryStatsRequest{} }
func (m *queryStatsRequest) String() string { return legacyproto.CompactTextString(m) }
func (*queryStatsRequest) ProtoMessage()    {}

type queryStatsResponse struct {
	Stat []*statsEntry `protobuf:"bytes,1,rep,name=stat,proto3" json:"stat,omitempty"`
}

func (m *queryStatsResponse) Reset()         { *m = queryStatsResponse{} }
func (m *queryStatsResponse) String() string { return legacyproto.CompactTextString(m) }
func (*queryStatsResponse) ProtoMessage()    {}

type statsEntry struct {
	Name  string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Value int64  `protobuf:"varint,2,opt,name=value,proto3" json:"value,omitempty"`
}

func (m *statsEntry) Reset()         { *m = statsEntry{} }
func (m *statsEntry) String() string { return legacyproto.CompactTextString(m) }
func (*statsEntry) ProtoMessage()    {}

type legacyProtoCodec struct{}

func (legacyProtoCodec) Marshal(v any) ([]byte, error) {
	message, ok := v.(legacyproto.Message)
	if !ok {
		return nil, fmt.Errorf("expected protobuf message, got %T", v)
	}
	return legacyproto.Marshal(message)
}

func (legacyProtoCodec) Unmarshal(data []byte, v any) error {
	message, ok := v.(legacyproto.Message)
	if !ok {
		return fmt.Errorf("expected protobuf message, got %T", v)
	}
	return legacyproto.Unmarshal(data, message)
}

func (legacyProtoCodec) Name() string {
	return "proto"
}

func main() {
	apiURL := flag.String("api-url", "http://localhost:8080", "API base URL")
	nodeID := flag.String("node-id", "", "Exit node ID")
	runtimeDir := flag.String("runtime-dir", ".runtime/agent", "runtime directory")
	interval := flag.Duration("interval", 10*time.Second, "poll interval")
	checkConfig := flag.Bool("check-config", false, "run sing-box check after writing config")
	singBoxBin := flag.String("sing-box-bin", "sing-box", "sing-box binary path")
	statsMode := flag.String("stats-mode", "auto", "traffic collector mode: auto, mock, or v2ray-api")
	v2rayReset := flag.Bool("v2ray-reset", true, "reset V2Ray API counters after each successful query")
	once := flag.Bool("once", false, "run one iteration and exit")
	flag.Parse()

	if *nodeID == "" {
		log.Fatal("--node-id is required")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var lastVersion int64
	for {
		cfg, err := fetchDesired(client, *apiURL, *nodeID)
		if err != nil {
			log.Printf("fetch desired config: %v", err)
		} else {
			if cfg.Version != lastVersion {
				configPath, err := writeConfig(*runtimeDir, cfg)
				if err != nil {
					log.Printf("write config: %v", err)
				} else {
					if *checkConfig {
						if err := validateConfig(*singBoxBin, configPath); err != nil {
							log.Printf("sing-box check failed: %v", err)
							if *once {
								return
							}
							time.Sleep(*interval)
							continue
						}
					}
					lastVersion = cfg.Version
					log.Printf("wrote desired config version %d", cfg.Version)
				}
			}
			if err := postHeartbeat(client, *apiURL, *nodeID); err != nil {
				log.Printf("heartbeat: %v", err)
			}
			events, err := selectCollector(*statsMode, *v2rayReset, cfg).Collect(context.Background(), cfg)
			if err != nil {
				log.Printf("collect traffic: %v", err)
			} else if err := postTraffic(client, *apiURL, *nodeID, events); err != nil {
				log.Printf("traffic: %v", err)
			}
		}
		if *once {
			return
		}
		time.Sleep(*interval)
	}
}

func selectCollector(mode string, reset bool, cfg desiredConfig) collector {
	if mode == "auto" || mode == "" {
		mode = cfg.StatsMode
	}
	switch mode {
	case "v2ray-api":
		return v2rayAPICollector{reset: reset}
	default:
		return mockCollector{}
	}
}

func fetchDesired(client *http.Client, apiURL, nodeID string) (desiredConfig, error) {
	var cfg desiredConfig
	resp, err := client.Get(fmt.Sprintf("%s/api/agent/%s/desired-config", apiURL, nodeID))
	if err != nil {
		return cfg, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return cfg, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	err = json.NewDecoder(resp.Body).Decode(&cfg)
	return cfg, err
}

func writeConfig(runtimeDir string, cfg desiredConfig) (string, error) {
	nodeDir := filepath.Join(runtimeDir, cfg.NodeID)
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg.SingBoxConfig, "", "  ")
	if err != nil {
		return "", err
	}
	configPath := filepath.Join(nodeDir, "sing-box.json")
	return configPath, os.WriteFile(configPath, data, 0o644)
}

func validateConfig(singBoxBin, configPath string) error {
	cmd := exec.Command(singBoxBin, "check", "-c", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func postHeartbeat(client *http.Client, apiURL, nodeID string) error {
	resp, err := client.Post(fmt.Sprintf("%s/api/agent/%s/heartbeat", apiURL, nodeID), "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (mockCollector) Collect(_ context.Context, cfg desiredConfig) ([]trafficEvent, error) {
	events := make([]trafficEvent, 0, len(cfg.TrackedUserIDs))
	for _, userID := range cfg.TrackedUserIDs {
		events = append(events, trafficEvent{
			UserID:        userID,
			UploadBytes:   int64(512 + rand.Intn(4096)),
			DownloadBytes: int64(1024 + rand.Intn(8192)),
			Source:        "mock-v2ray-stats",
		})
	}
	return events, nil
}

func (c v2rayAPICollector) Collect(ctx context.Context, cfg desiredConfig) ([]trafficEvent, error) {
	if len(cfg.TrackedUserIDs) == 0 {
		return nil, nil
	}
	if cfg.StatsAPITarget == "" {
		return nil, fmt.Errorf("stats_api_target is required for v2ray-api mode")
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(callCtx, cfg.StatsAPITarget, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp := &queryStatsResponse{}
	err = conn.Invoke(callCtx, "/v2ray.core.app.stats.command.StatsService/QueryStats", &queryStatsRequest{
		Pattern: "user>>>",
		Reset_:  c.reset,
	}, resp, grpc.ForceCodec(legacyProtoCodec{}))
	if err != nil {
		return nil, err
	}
	stats := make([]v2rayStat, 0, len(resp.Stat))
	for _, stat := range resp.Stat {
		stats = append(stats, v2rayStat{Name: stat.Name, Value: stat.Value})
	}
	return parseV2RayStats(stats, cfg.TrackedUserIDs), nil
}

func parseV2RayStats(stats []v2rayStat, userIDs []string) []trafficEvent {
	wanted := make(map[string]struct{}, len(userIDs))
	byUser := make(map[string]*trafficEvent, len(userIDs))
	for _, userID := range userIDs {
		wanted[userID] = struct{}{}
		byUser[userID] = &trafficEvent{UserID: userID, Source: "v2ray-api"}
	}
	for _, stat := range stats {
		parts := strings.Split(stat.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		userID := parts[1]
		if _, ok := wanted[userID]; !ok {
			continue
		}
		switch parts[3] {
		case "uplink":
			byUser[userID].UploadBytes += stat.Value
		case "downlink":
			byUser[userID].DownloadBytes += stat.Value
		}
	}
	events := make([]trafficEvent, 0, len(userIDs))
	for _, userID := range userIDs {
		event := byUser[userID]
		if event.UploadBytes == 0 && event.DownloadBytes == 0 {
			continue
		}
		events = append(events, *event)
	}
	return events
}

func postTraffic(client *http.Client, apiURL, nodeID string, events []trafficEvent) error {
	if len(events) == 0 {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"events": events})
	resp, err := client.Post(fmt.Sprintf("%s/api/agent/%s/traffic", apiURL, nodeID), "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
