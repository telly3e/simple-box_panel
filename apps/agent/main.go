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

type heartbeatStatus struct {
	AppliedConfigVersion int64  `json:"applied_config_version,omitempty"`
	LastError            string `json:"last_error,omitempty"`
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
	singBoxService := flag.String("sing-box-service", envDefault("SING_PANEL_SING_BOX_SERVICE", ""), "optional systemd service to restart after applying a valid config")
	statsMode := flag.String("stats-mode", "auto", "traffic collector mode: auto, mock, or v2ray-api")
	v2rayReset := flag.Bool("v2ray-reset", true, "reset V2Ray API counters after each successful query")
	apiBasicUser := flag.String("api-basic-user", envDefault("SING_PANEL_API_BASIC_USER", ""), "optional API basic auth username")
	apiBasicPass := flag.String("api-basic-pass", envDefault("SING_PANEL_API_BASIC_PASS", ""), "optional API basic auth password")
	agentToken := flag.String("agent-token", envDefault("SING_PANEL_AGENT_TOKEN", ""), "agent token for this Exit node")
	applyOnly := flag.Bool("apply-only", false, "write, validate, and optionally restart config without collecting traffic")
	once := flag.Bool("once", false, "run one iteration and exit")
	flag.Parse()

	if *nodeID == "" {
		log.Fatal("--node-id is required")
	}

	client := apiClient{
		base:       &http.Client{Timeout: 10 * time.Second},
		basicUser:  *apiBasicUser,
		basicPass:  *apiBasicPass,
		agentToken: *agentToken,
	}
	var appliedVersion int64
	var lastError string
	for {
		cfg, err := fetchDesired(client, *apiURL, *nodeID)
		if err != nil {
			lastError = fmt.Sprintf("fetch desired config: %v", err)
			log.Print(lastError)
			if err := postHeartbeat(client, *apiURL, *nodeID, heartbeatStatus{AppliedConfigVersion: appliedVersion, LastError: lastError}); err != nil {
				log.Printf("heartbeat: %v", err)
			}
		} else {
			if cfg.Version != appliedVersion {
				if _, err := applyDesiredConfig(*runtimeDir, cfg, *checkConfig, *singBoxBin, *singBoxService); err != nil {
					lastError = fmt.Sprintf("apply config version %d: %v", cfg.Version, err)
					log.Print(lastError)
				} else {
					appliedVersion = cfg.Version
					lastError = ""
					log.Printf("applied desired config version %d", cfg.Version)
				}
			}
			if !*applyOnly {
				events, err := selectCollector(*statsMode, *v2rayReset, cfg).Collect(context.Background(), cfg)
				if err != nil {
					lastError = fmt.Sprintf("collect traffic: %v", err)
					log.Print(lastError)
				} else if err := postTraffic(client, *apiURL, *nodeID, events); err != nil {
					lastError = fmt.Sprintf("traffic: %v", err)
					log.Print(lastError)
				} else if cfg.Version == appliedVersion {
					lastError = ""
				}
			}
			if err := postHeartbeat(client, *apiURL, *nodeID, heartbeatStatus{AppliedConfigVersion: appliedVersion, LastError: lastError}); err != nil {
				log.Printf("heartbeat: %v", err)
			}
		}
		if *once {
			return
		}
		time.Sleep(*interval)
	}
}

func envDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

type apiClient struct {
	base       *http.Client
	basicUser  string
	basicPass  string
	agentToken string
}

func (c apiClient) Do(req *http.Request) (*http.Response, error) {
	if c.basicUser != "" || c.basicPass != "" {
		req.SetBasicAuth(c.basicUser, c.basicPass)
	}
	if c.agentToken != "" {
		req.Header.Set("X-Sing-Panel-Agent-Token", c.agentToken)
	}
	return c.base.Do(req)
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

func fetchDesired(client apiClient, apiURL, nodeID string) (desiredConfig, error) {
	var cfg desiredConfig
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/agent/%s/desired-config", apiURL, nodeID), nil)
	if err != nil {
		return cfg, err
	}
	resp, err := client.Do(req)
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
	if err := resolveEnvPlaceholders(cfg.SingBoxConfig); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg.SingBoxConfig, "", "  ")
	if err != nil {
		return "", err
	}
	configPath := filepath.Join(nodeDir, "sing-box.json")
	return configPath, os.WriteFile(configPath, data, 0o644)
}

func applyDesiredConfig(runtimeDir string, cfg desiredConfig, checkConfig bool, singBoxBin, service string) (string, error) {
	configPath, err := writeConfig(runtimeDir, cfg)
	if err != nil {
		return "", err
	}
	if checkConfig {
		if err := validateConfig(singBoxBin, configPath); err != nil {
			return "", fmt.Errorf("sing-box check failed: %w", err)
		}
	}
	if service != "" {
		if err := restartSystemdService(service); err != nil {
			return "", err
		}
	}
	return configPath, nil
}

func resolveEnvPlaceholders(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			if err := resolveEnvPlaceholders(child); err != nil {
				return err
			}
		}
		for key, raw := range typed {
			if !strings.HasSuffix(key, "_env") {
				continue
			}
			envName, ok := raw.(string)
			if !ok || envName == "" {
				return fmt.Errorf("%s must name an environment variable", key)
			}
			envValue := os.Getenv(envName)
			if envValue == "" {
				return fmt.Errorf("environment variable %s is required for %s", envName, key)
			}
			typed[strings.TrimSuffix(key, "_env")] = envValue
			delete(typed, key)
		}
	case []any:
		for _, child := range typed {
			if err := resolveEnvPlaceholders(child); err != nil {
				return err
			}
		}
	case []map[string]any:
		for _, child := range typed {
			if err := resolveEnvPlaceholders(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateConfig(singBoxBin, configPath string) error {
	cmd := exec.Command(singBoxBin, "check", "-c", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func restartSystemdService(service string) error {
	cmd := exec.Command("systemctl", "restart", service)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart %s: %w: %s", service, err, string(out))
	}
	return nil
}

func postHeartbeat(client apiClient, apiURL, nodeID string, status heartbeatStatus) error {
	payload, err := json.Marshal(status)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/agent/%s/heartbeat", apiURL, nodeID), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
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

func postTraffic(client apiClient, apiURL, nodeID string, events []trafficEvent) error {
	if len(events) == 0 {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"events": events})
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/agent/%s/traffic", apiURL, nodeID), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
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
