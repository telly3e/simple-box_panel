package api

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"singpanel/internal/configgen"
	"singpanel/internal/domain"
	"singpanel/internal/store"
)

type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

func NewServer(s *store.Store) http.Handler {
	return NewServerWithWeb(s, "")
}

func NewServerWithWeb(s *store.Store, webDir string) http.Handler {
	server := &Server{store: s, mux: http.NewServeMux()}
	server.routes()
	if webDir != "" {
		server.mux.HandleFunc("/", serveWeb(webDir))
	}
	return cors(server.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.health)
	s.mux.HandleFunc("/api/summary", s.summary)
	s.mux.HandleFunc("/api/users", s.users)
	s.mux.HandleFunc("/api/users/", s.userByID)
	s.mux.HandleFunc("/api/exit-nodes", s.exitNodes)
	s.mux.HandleFunc("/api/exit-nodes/", s.exitNodeByID)
	s.mux.HandleFunc("/api/agent/", s.agent)
	s.mux.HandleFunc("/sub/", s.subscription)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	summary, err := s.store.Summary(r.Context())
	respond(w, summary, err)
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := s.store.ListUsers(r.Context())
		respond(w, users, err)
	case http.MethodPost:
		var req struct {
			Name       string `json:"name"`
			QuotaBytes int64  `json:"quota_bytes"`
		}
		if !decode(w, r, &req) {
			return
		}
		user, err := s.store.CreateUser(r.Context(), req.Name, req.QuotaBytes)
		respondCreated(w, user, err)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) userByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if id == "" || strings.Contains(id, "/") {
		notFound(w)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var patch store.UserPatch
		if !decode(w, r, &patch) {
			return
		}
		user, err := s.store.PatchUser(r.Context(), id, patch)
		respond(w, user, err)
	case http.MethodDelete:
		if err := s.store.DeleteUser(r.Context(), id); err != nil {
			respond(w, map[string]string{}, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) exitNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		nodes, err := s.store.ListExitNodes(r.Context())
		respond(w, nodes, err)
	case http.MethodPost:
		var req domain.ExitNode
		if !decode(w, r, &req) {
			return
		}
		node, err := s.store.CreateExitNode(r.Context(), req)
		respondCreated(w, node, err)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) exitNodeByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/exit-nodes/")
	if id == "" || strings.Contains(id, "/") {
		notFound(w)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var patch store.ExitNodePatch
		if !decode(w, r, &patch) {
			return
		}
		node, err := s.store.PatchExitNode(r.Context(), id, patch)
		respond(w, node, err)
	case http.MethodDelete:
		if err := s.store.DeleteExitNode(r.Context(), id); err != nil {
			respond(w, map[string]string{}, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) subscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/sub/")
	if token == "" || strings.Contains(token, "/") {
		notFound(w)
		return
	}
	user, err := s.store.GetUserBySubscriptionToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusNotFound, "subscription not found")
		return
	}
	if !user.Active() {
		writeError(w, http.StatusForbidden, "user is disabled or over quota")
		return
	}
	nodes, err := s.store.ListExitNodes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, configgen.GenerateClientSubscription(user, nodes))
}

func (s *Server) agent(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/agent/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		notFound(w)
		return
	}
	nodeID, action := parts[0], parts[1]
	node, err := s.store.GetExitNode(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "node not found")
		return
	}
	if !validAgentToken(r, node.AgentToken) {
		writeError(w, http.StatusUnauthorized, "invalid agent token")
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "desired-config":
		users, err := s.store.ActiveUsers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, configgen.GenerateServerConfig(node, users))
	case r.Method == http.MethodPost && action == "heartbeat":
		var req store.HeartbeatInput
		if !decode(w, r, &req) {
			return
		}
		err := s.store.RecordHeartbeat(r.Context(), nodeID, req)
		respond(w, map[string]string{"status": "ok"}, err)
	case r.Method == http.MethodPost && action == "traffic":
		var req struct {
			Events []store.TrafficInput `json:"events"`
		}
		if !decode(w, r, &req) {
			return
		}
		if len(req.Events) == 0 {
			writeError(w, http.StatusBadRequest, "events is required")
			return
		}
		err := s.store.RecordTraffic(r.Context(), nodeID, req.Events)
		respond(w, map[string]string{"status": "ok"}, err)
	default:
		methodNotAllowed(w)
	}
}

func validAgentToken(r *http.Request, want string) bool {
	if want == "" {
		return false
	}
	got := r.Header.Get("X-Sing-Panel-Agent-Token")
	if got == "" {
		auth := r.Header.Get("Authorization")
		if token, ok := strings.CutPrefix(auth, "Bearer "); ok {
			got = strings.TrimSpace(token)
		}
	}
	if got == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func respond[T any](w http.ResponseWriter, value T, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func respondCreated[T any](w http.ResponseWriter, value T, err error) {
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not found")
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Sing-Panel-Agent-Token")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveWeb(webDir string) http.HandlerFunc {
	fs := http.Dir(webDir)
	fileServer := http.FileServer(fs)
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			notFound(w)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w)
			return
		}
		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if cleanPath == "." {
			cleanPath = "index.html"
		}
		if cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) || filepath.IsAbs(cleanPath) {
			notFound(w)
			return
		}
		if info, err := os.Stat(filepath.Join(webDir, cleanPath)); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	}
}
