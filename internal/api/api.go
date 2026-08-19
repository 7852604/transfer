package api

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"transfer/internal/backup"
	"transfer/internal/store"
	"transfer/internal/webui"
)

type Config struct {
	Password     string
	CookieSecure bool
	DataDir      string
	UploadsDir   string
	DBPath       string
	MaxFileBytes int64
	MaxTextLen   int
}

type Server struct {
	cfg     Config
	store   *store.Store
	backups *backup.Manager
	limiter loginLimiter
}

func New(cfg Config, st *store.Store, bk *backup.Manager) *Server {
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 50 << 20
	}
	if cfg.MaxTextLen <= 0 {
		cfg.MaxTextLen = 64 * 1024
	}
	return &Server{cfg: cfg, store: st, backups: bk}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.auth(s.handleLogout))

	mux.Handle("GET /api/messages", s.auth(s.handleListMessages))
	mux.Handle("POST /api/messages", s.auth(s.handlePostMessage))
	mux.Handle("POST /api/upload", s.auth(s.handleUpload))
	mux.Handle("GET /api/files/{fileId}", s.auth(s.handleServeFile))
	mux.Handle("DELETE /api/messages/{id}", s.auth(s.handleDeleteMessage))
	mux.Handle("POST /api/cleanup", s.auth(s.handleCleanup))
	mux.Handle("POST /api/clear", s.auth(s.handleClear))
	mux.Handle("GET /api/search", s.auth(s.handleSearch))
	mux.Handle("GET /api/stats", s.auth(s.handleStats))
	mux.Handle("POST /api/backup", s.auth(s.handleBackupNow))

	mux.Handle("/", s.spa())

	return logRequest(securityHeaders(mux))
}

// ---------- 消息 ----------

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := clampInt(atoi(q.Get("limit")), 1, 500, 200)
	after := atoi64(q.Get("after"))
	before := atoi64(q.Get("before"))
	if after > 0 && before > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "after 和 before 不能同时传"})
		return
	}
	msgs, hasMore, err := s.store.List(after, before, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取消息失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "hasMore": hasMore})
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(s.cfg.MaxTextLen)*4)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求格式错误"})
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "内容不能为空"})
		return
	}
	// rune 计数限制，防止超长文本拖垮渲染
	if len([]rune(content)) > s.cfg.MaxTextLen {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "文字太长了"})
		return
	}
	msg, err := s.store.InsertText(content)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存失败"})
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "非法 id"})
		return
	}
	msg, err := s.store.Delete(id)
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "消息不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "删除失败"})
		return
	}
	s.removeMessageFile(msg)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 批量清理 ----------

func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Days int `json:"days"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.Days <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请指定要清理多少天前的消息"})
		return
	}
	ts := time.Now().AddDate(0, 0, -body.Days).UnixMilli()
	msgs, err := s.store.DeleteBefore(ts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "清理失败"})
		return
	}
	var freed int64
	for _, m := range msgs {
		freed += m.FileSize
		s.removeMessageFile(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": len(msgs), "freedBytes": freed})
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	msgs, err := s.store.DeleteAll()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "清空失败"})
		return
	}
	var freed int64
	for _, m := range msgs {
		freed += m.FileSize
		s.removeMessageFile(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": len(msgs), "freedBytes": freed})
}

// ---------- 搜索 / 统计 / 备份 ----------

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"messages": []store.Message{}})
		return
	}
	msgs, err := s.store.Search(q, 200)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "搜索失败"})
		return
	}
	if msgs == nil {
		msgs = []store.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	count, fileBytes, err := s.store.Stats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取统计失败"})
		return
	}
	dbBytes := int64(0)
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		dbBytes += fi.Size()
	}
	if fi, err := os.Stat(s.cfg.DBPath + "-wal"); err == nil {
		dbBytes += fi.Size()
	}
	stats := map[string]any{
		"count":     count,
		"fileBytes": fileBytes,
		"dbBytes":   dbBytes,
	}
	if name, size, at, ok := s.backups.Last(); ok {
		stats["lastBackup"] = map[string]any{"name": name, "size": size, "at": at.UnixMilli()}
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleBackupNow(w http.ResponseWriter, r *http.Request) {
	name, size, err := s.backups.Run()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "备份失败: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "size": size})
}

// ---------- 静态资源（SPA） ----------

func (s *Server) spa() http.Handler {
	dist := webui.Dist()
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(dist, p); err != nil {
			// 前端是单页应用，未命中的一律回退到 index.html
			http.ServeFileFS(w, r, dist, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// ---------- 中间件与工具 ----------

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			sw := &statusWriter{ResponseWriter: w, status: 200}
			start := time.Now()
			next.ServeHTTP(sw, r)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func atoi(s string) int     { n, _ := strconv.Atoi(s); return n }
func atoi64(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
func clampInt(n, lo, hi, def int) int {
	if n == 0 {
		return def
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
