package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const dynamicVariableCacheTTL = 30 * time.Second

// Server serves a local editable dashboard.
type Server struct {
	FilePath      string
	Dashboards    []DashboardRef
	Client        QueryClient
	Host          string
	Token         string
	variableMu    sync.Mutex
	variableCache map[string]variableCacheEntry
}

type variableCacheEntry struct {
	ETag      string
	Variables map[string]Variable
	ExpiresAt time.Time
}

type dashboardResponse struct {
	Dashboard   *Dashboard     `json:"dashboard"`
	Dashboards  []DashboardRef `json:"dashboards"`
	DashboardID string         `json:"dashboard_id"`
	Raw         string         `json:"raw"`
	ETag        string         `json:"etag"`
}

type saveRequest struct {
	Raw  string `json:"raw"`
	ETag string `json:"etag"`
}

type runRequest struct {
	DashboardID string            `json:"dashboard_id"`
	Variables   map[string]string `json:"variables"`
	CardIDs     []string          `json:"card_ids"`
}

// NewServer creates a dashboard server.
func NewServer(filePath, host string, qc QueryClient) (*Server, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	refs, err := DiscoverDashboardFiles(filePath)
	if err != nil {
		return nil, err
	}
	if host == "" {
		host = "http://127.0.0.1"
	}
	return &Server{
		FilePath:      filePath,
		Dashboards:    refs,
		Host:          host,
		Client:        qc,
		Token:         token,
		variableCache: make(map[string]variableCacheEntry),
	}, nil
}

// Handler returns the local dashboard HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/dashboard", s.secure(s.handleDashboard))
	mux.HandleFunc("/api/run", s.secure(s.handleRun))
	mux.HandleFunc("/api/export", s.secure(s.handleExport))
	return s.hostGuard(mux)
}

// ListenAndServe serves the dashboard on addr until ctx is canceled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errs := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		if err != nil {
			return fmt.Errorf("shutdown dashboard server: %w", err)
		}
		return ctx.Err()
	case err := <-errs:
		return err
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	ref, err := s.dashboardRef("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d, _, err := s.loadDashboard(ref.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	html, err := RenderHTML(d, nil, ExportOptions{
		Mode:         ExportLocal,
		Host:         s.Host,
		SessionToken: s.Token,
		Dashboards:   s.Dashboards,
		DashboardID:  ref.ID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ref, err := s.dashboardRef(r.URL.Query().Get("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		d, raw, err := s.loadDashboard(ref.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err = s.resolveDynamicVariables(r.Context(), ref.ID, raw, d); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, dashboardResponse{
			Dashboard:   d,
			Dashboards:  s.Dashboards,
			DashboardID: ref.ID,
			Raw:         string(raw),
			ETag:        FileETag(raw),
		})
	case http.MethodPut:
		ref, err := s.dashboardRef(r.URL.Query().Get("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		var req saveRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		d, _, err := Load(strings.NewReader(req.Raw))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err = Validate(d); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		err = SaveAtomic(ref.Path, []byte(req.Raw), req.ETag)
		if err != nil {
			if errors.Is(err, ErrETagMismatch) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"etag": FileETag([]byte(req.Raw))})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req runRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	d, raw, err := s.loadDashboard(req.DashboardID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err = s.resolveDynamicVariables(r.Context(), req.DashboardID, raw, d); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cardIDs := make(map[string]struct{}, len(req.CardIDs))
	for _, id := range req.CardIDs {
		cardIDs[id] = struct{}{}
	}
	results, err := RunAll(r.Context(), s.Client, d, RunOptions{Variables: req.Variables, CardIDs: cardIDs})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"results": results})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d, raw, err := s.loadDashboard("")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err = s.resolveDynamicVariables(r.Context(), "", raw, d); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	results, err := RunAll(r.Context(), s.Client, d, RunOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	html, err := RenderHTML(d, results, ExportOptions{Mode: ExportReport})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(html)
}

func (s *Server) loadDashboard(id string) (*Dashboard, []byte, error) {
	ref, err := s.dashboardRef(id)
	if err != nil {
		return nil, nil, err
	}
	d, raw, err := LoadFile(ref.Path)
	if err != nil {
		return nil, nil, err
	}
	if err = Validate(d); err != nil {
		return nil, nil, err
	}
	return d, raw, nil
}

func (s *Server) resolveDynamicVariables(ctx context.Context, dashboardID string, raw []byte, d *Dashboard) error {
	if !hasDynamicVariables(d) {
		return Validate(d)
	}
	if dashboardID == "" {
		if ref, err := s.dashboardRef(""); err == nil {
			dashboardID = ref.ID
		}
	}
	etag := FileETag(raw)
	now := time.Now()
	s.variableMu.Lock()
	if entry, ok := s.variableCache[dashboardID]; ok && entry.ETag == etag && now.Before(entry.ExpiresAt) {
		d.Variables = cloneVariables(entry.Variables)
		s.variableMu.Unlock()
		return Validate(d)
	}
	s.variableMu.Unlock()

	if err := ResolveDynamicVariables(ctx, s.Client, d); err != nil {
		return err
	}

	s.variableMu.Lock()
	s.variableCache[dashboardID] = variableCacheEntry{
		ETag:      etag,
		Variables: cloneVariables(d.Variables),
		ExpiresAt: now.Add(dynamicVariableCacheTTL),
	}
	s.variableMu.Unlock()
	return nil
}

func hasDynamicVariables(d *Dashboard) bool {
	if d == nil {
		return false
	}
	for _, variable := range d.Variables {
		if strings.TrimSpace(variable.Query) != "" {
			return true
		}
	}
	return false
}

func cloneVariables(in map[string]Variable) map[string]Variable {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]Variable, len(in))
	for name, variable := range in {
		variable.Options = append([]VariableOption(nil), variable.Options...)
		out[name] = variable
	}
	return out
}

func (s *Server) dashboardRef(id string) (DashboardRef, error) {
	if len(s.Dashboards) == 0 {
		return DashboardRef{}, fmt.Errorf("no dashboards configured")
	}
	if id == "" {
		return s.Dashboards[0], nil
	}
	for _, ref := range s.Dashboards {
		if ref.ID == id {
			return ref, nil
		}
	}
	return DashboardRef{}, fmt.Errorf("dashboard %q not found", id)
}

func (s *Server) secure(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-WireLog-Dashboard-Token") != s.Token {
			http.Error(w, "dashboard token required", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != localOrigin(r) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		referer := r.Header.Get("Referer")
		if referer != "" && !strings.HasPrefix(referer, localOrigin(r)) {
			http.Error(w, "referer not allowed", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "localhost" && host != "127.0.0.1" && host != "[::1]" && host != "::1" {
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func localOrigin(r *http.Request) string {
	scheme := "http://"
	if r.TLS != nil {
		scheme = "https://"
	}
	return scheme + r.Host
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer func() {
		_ = r.Body.Close()
	}()
	err := json.NewDecoder(r.Body).Decode(dest)
	if err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func randomToken() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return "", fmt.Errorf("generate dashboard token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// FirstFreeAddr returns a loopback address with an OS-assigned free port.
func FirstFreeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen on loopback: %w", err)
	}
	defer func() {
		_ = l.Close()
	}()
	return l.Addr().String(), nil
}

// WriteNewFile writes data to path unless it exists and force is false.
func WriteNewFile(path string, data []byte, force bool) error {
	if path == "-" {
		_, err := os.Stdout.Write(data)
		if err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; use --force to overwrite", path)
		}
	}
	err := os.WriteFile(path, data, 0o644)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}
