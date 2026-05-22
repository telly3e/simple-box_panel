package api

import (
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
	s.mux.HandleFunc("/api/entry-nodes", s.entryNodes)
	s.mux.HandleFunc("/api/entry-nodes/", s.entryNodeByID)
	s.mux.HandleFunc("/api/subscriptions/", s.subscription)
	s.mux.HandleFunc("/api/agent/", s.agent)
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
	if r.Method != http.MethodPatch {
		methodNotAllowed(w)
		return
	}
	var patch store.ExitNodePatch
	if !decode(w, r, &patch) {
		return
	}
	node, err := s.store.PatchExitNode(r.Context(), id, patch)
	respond(w, node, err)
}

func (s *Server) entryNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		nodes, err := s.store.ListEntryNodes(r.Context())
		respond(w, nodes, err)
	case http.MethodPost:
		var req domain.EntryNode
		if !decode(w, r, &req) {
			return
		}
		node, err := s.store.CreateEntryNode(r.Context(), req)
		respondCreated(w, node, err)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) entryNodeByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/entry-nodes/")
	if id == "" || strings.Contains(id, "/") {
		notFound(w)
		return
	}
	if r.Method != http.MethodPatch {
		methodNotAllowed(w)
		return
	}
	var patch store.EntryNodePatch
	if !decode(w, r, &patch) {
		return
	}
	node, err := s.store.PatchEntryNode(r.Context(), id, patch)
	respond(w, node, err)
}

func (s *Server) subscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/subscriptions/")
	userID, ok := strings.CutSuffix(rest, "/sing-box.json")
	if !ok || userID == "" {
		notFound(w)
		return
	}
	user, err := s.store.GetUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if !user.Active() {
		writeError(w, http.StatusForbidden, "user is disabled or over quota")
		return
	}
	entries, err := s.store.ListEntryNodes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, configgen.GenerateClientSubscription(user, entries))
}

func (s *Server) agent(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/agent/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		notFound(w)
		return
	}
	nodeID, action := parts[0], parts[1]
	switch {
	case r.Method == http.MethodGet && action == "desired-config":
		node, err := s.store.GetExitNode(r.Context(), nodeID)
		if err != nil {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		users, err := s.store.ActiveUsers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, configgen.GenerateServerConfig(node, users))
	case r.Method == http.MethodPost && action == "heartbeat":
		err := s.store.RecordHeartbeat(r.Context(), nodeID)
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
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
