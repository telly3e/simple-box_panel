package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type desiredConfig struct {
	NodeID         string         `json:"node_id"`
	Version        int64          `json:"version"`
	SingBoxConfig  map[string]any `json:"sing_box_config"`
	TrackedUserIDs []string       `json:"tracked_user_ids"`
	StatsMode      string         `json:"stats_mode"`
}

type trafficEvent struct {
	UserID        string `json:"user_id"`
	UploadBytes   int64  `json:"upload_bytes"`
	DownloadBytes int64  `json:"download_bytes"`
	Source        string `json:"source"`
}

func main() {
	apiURL := flag.String("api-url", "http://localhost:8080", "API base URL")
	nodeID := flag.String("node-id", "", "Exit node ID")
	runtimeDir := flag.String("runtime-dir", ".runtime/agent", "runtime directory")
	interval := flag.Duration("interval", 10*time.Second, "poll interval")
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
				if err := writeConfig(*runtimeDir, cfg); err != nil {
					log.Printf("write config: %v", err)
				} else {
					lastVersion = cfg.Version
					log.Printf("wrote desired config version %d", cfg.Version)
				}
			}
			if err := postHeartbeat(client, *apiURL, *nodeID); err != nil {
				log.Printf("heartbeat: %v", err)
			}
			if err := postMockTraffic(client, *apiURL, *nodeID, cfg.TrackedUserIDs); err != nil {
				log.Printf("traffic: %v", err)
			}
		}
		if *once {
			return
		}
		time.Sleep(*interval)
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

func writeConfig(runtimeDir string, cfg desiredConfig) error {
	nodeDir := filepath.Join(runtimeDir, cfg.NodeID)
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg.SingBoxConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(nodeDir, "sing-box.json"), data, 0o644)
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

func postMockTraffic(client *http.Client, apiURL, nodeID string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	events := make([]trafficEvent, 0, len(userIDs))
	for _, userID := range userIDs {
		events = append(events, trafficEvent{
			UserID:        userID,
			UploadBytes:   int64(512 + rand.Intn(4096)),
			DownloadBytes: int64(1024 + rand.Intn(8192)),
			Source:        "mock-v2ray-stats",
		})
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
