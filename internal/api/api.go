package api

import (
	"context"
	"crypto/subtle"
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
	DataDir       string
	UploadsDir    string
	DBPath        string
	MaxFileBytes  int64
	MaxTextLen    int
	CookieSecure  bool
	WebhookToken  string // BUG反馈房间外部调用 Token
	AdminPassword string // 删除房间等管理操作的全局密码；为空则管理操作禁用
}

type Server struct {
	cfg     Config
	store   *store.Store
	backups *backup.Manager
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

const sessionCookie = "room_session"

// clientIP 提取客户端 IP（nginx 反代场景优先 X-Forwarded-For 首段）
func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if i := strings.IndexByte(xf, ','); i > 0 {
			return strings.TrimSpace(xf[:i])
		}
		return strings.TrimSpace(xf)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return strings.TrimSpace(xr)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		host = host[:i]
	}
	return host
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 房间管理
	mux.HandleFunc("GET /api/rooms", s.handleListRooms)
	mux.HandleFunc("POST /api/rooms", s.handleCreateRoom)
	mux.HandleFunc("POST /api/rooms/{room}/login", s.handleRoomLogin)
	mux.HandleFunc("POST /api/rooms/logout", s.handleRoomLogout)
	mux.Handle("POST /api/rooms/password", s.roomAuth(s.handleSetRoomPassword))
	mux.Handle("DELETE /api/rooms/current", s.roomAuth(s.handleDeleteRoom))
	mux.Handle("POST /api/rooms/pin", s.roomAuth(s.handlePinMessage))

	// 房间内操作（需要 room session）
	mux.Handle("GET /api/messages", s.roomAuth(s.handleListMessages))
	mux.Handle("POST /api/messages", s.roomAuth(s.handlePostMessage))
	mux.Handle("POST /api/upload", s.roomAuth(s.handleUpload))
	mux.Handle("GET /api/files/{fileId}", s.roomAuth(s.handleServeFile))
	mux.Handle("DELETE /api/messages/{id}", s.roomAuth(s.handleDeleteMessage))

	// 回收站
	mux.Handle("GET /api/trash", s.roomAuth(s.handleListTrash))
	mux.Handle("POST /api/trash/{id}/restore", s.roomAuth(s.handleRestoreMessage))
	mux.Handle("DELETE /api/trash/{id}", s.roomAuth(s.handlePermanentDelete))
	mux.Handle("POST /api/trash/empty", s.roomAuth(s.handleEmptyTrash))

	// 批量操作
	mux.Handle("POST /api/cleanup", s.roomAuth(s.handleCleanup))
	mux.Handle("POST /api/clear", s.roomAuth(s.handleClear))
	mux.Handle("GET /api/search", s.roomAuth(s.handleSearch))
	mux.Handle("GET /api/stats", s.roomAuth(s.handleStats))

	// 外部 webhook：BUG反馈房间
	mux.HandleFunc("POST /api/webhook/message", s.handleWebhookMessage)
	mux.HandleFunc("POST /api/webhook/upload", s.handleWebhookUpload)

	// 备份
	mux.Handle("POST /api/backup", s.roomAuth(s.handleBackupNow))

	// 静态资源
	mux.Handle("/", s.spa())

	return logRequest(securityHeaders(mux))
}

// ---------- 房间鉴权 ----------

const roomCtxKey ctxKey = "room"

type ctxKey string

func contextWithRoom(ctx context.Context, room store.Room) context.Context {
	return context.WithValue(ctx, roomCtxKey, room)
}

func (s *Server) currentRoom(r *http.Request) (store.Room, error) {
	roomIDStr := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		roomIDStr = c.Value
	}
	if roomIDStr == "" {
		roomIDStr = r.Header.Get("X-Room-Id")
	}
	if roomIDStr == "" {
		return s.store.GetRoomByID(1) // 默认 BUG反馈
	}
	roomID, err := strconv.ParseInt(roomIDStr, 10, 64)
	if err != nil {
		return s.store.GetRoomByID(1)
	}
	return s.store.GetRoomByID(roomID)
}

func (s *Server) roomAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		room, err := s.currentRoom(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "请先进入房间"})
			return
		}
		ctx := r.Context()
		ctx = contextWithRoom(ctx, room)
		next(w, r.WithContext(ctx))
	}
}

func getRoom(r *http.Request) store.Room {
	if v, ok := r.Context().Value(roomCtxKey).(store.Room); ok {
		return v
	}
	return store.Room{}
}

// ---------- 房间管理 ----------

func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := s.store.ListRooms()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取房间失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求格式错误"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "房间名不能为空"})
		return
	}
	if len(name) > 30 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "房间名太长"})
		return
	}
	room, err := s.store.CreateRoom(name, strings.TrimSpace(body.Password))
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "房间名已存在"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: strconv.FormatInt(room.ID, 10),
		Path: "/", MaxAge: 86400 * 3650, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, room)
}

func (s *Server) handleRoomLogin(w http.ResponseWriter, r *http.Request) {
	roomName := r.PathValue("room")
	room, err := s.store.GetRoom(roomName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "房间不存在"})
		return
	}
	var body struct{ Password string `json:"password"` }
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	if room.HasPassword {
		ok, _ := s.store.CheckRoomPassword(room.ID, body.Password)
		if !ok {
			time.Sleep(200 * time.Millisecond)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "密码不对"})
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: strconv.FormatInt(room.ID, 10),
		Path: "/", MaxAge: 86400 * 3650, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.cfg.CookieSecure,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "room": room})
}

func (s *Server) handleRoomLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// 给当前房间设置/修改/清除密码。空字符串 = 取消加密。
// 已加密房间改密/清密时需先登录（roomAuth 已保证），故不另验旧密码。
// 但为防止匿名用户改 BUG反馈 房间，要求必须携带有效 room_session cookie。
func (s *Server) handleSetRoomPassword(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie(sessionCookie); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "请先进入房间"})
		return
	}
	room := getRoom(r)
	if room.ID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "房间无效"})
		return
	}
	var body struct{ Password string `json:"password"` }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求格式错误"})
		return
	}
	pw := strings.TrimSpace(body.Password)
	if len(pw) > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "密码太长"})
		return
	}
	if err := s.store.SetRoomPassword(room.ID, pw); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "设置密码失败"})
		return
	}
	updated, _ := s.store.GetRoomByID(room.ID)
	writeJSON(w, http.StatusOK, updated)
}

// delRoomAttempts 每分钟每 IP 最多 10 次删除房间尝试
var delRoomAttempts = map[string][]time.Time{}

func delRoomRateLimited(ip string) bool {
	now := time.Now()
	window := now.Add(-time.Minute)
	keep := delRoomAttempts[ip][:0]
	for _, t := range delRoomAttempts[ip] {
		if t.After(window) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= 10 {
		delRoomAttempts[ip] = keep
		return true
	}
	delRoomAttempts[ip] = append(keep, now)
	return false
}

// 删除当前房间（连同全部消息，不进回收站）。需要全局管理密码。
// ADMIN_PASSWORD 未配置时功能整体禁用。
func (s *Server) handleDeleteRoom(w http.ResponseWriter, r *http.Request) {
	room := getRoom(r)
	if s.cfg.AdminPassword == "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "未配置 ADMIN_PASSWORD，删除房间功能已禁用"})
		return
	}
	if room.ID == 1 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "默认房间不能删除"})
		return
	}
	if delRoomRateLimited(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "尝试太频繁，请稍后再试"})
		return
	}
	var body struct{ AdminPassword string `json:"adminPassword"` }
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	if subtle.ConstantTimeCompare([]byte(body.AdminPassword), []byte(s.cfg.AdminPassword)) != 1 {
		time.Sleep(200 * time.Millisecond)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "管理密码不对"})
		return
	}
	msgs, err := s.store.DeleteRoom(room.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "删除房间失败"})
		return
	}
	for _, m := range msgs {
		s.removeMessageFile(m)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": len(msgs)})
}

// 置顶/取消置顶当前房间的一条消息
func (s *Server) handlePinMessage(w http.ResponseWriter, r *http.Request) {
	room := getRoom(r)
	var body struct {
		MessageID int64 `json:"messageId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求格式错误"})
		return
	}
	// 取消置顶直接清
	if body.MessageID == 0 {
		if err := s.store.PinMessage(room.ID, 0); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "取消置顶失败"})
			return
		}
		updated, _ := s.store.GetRoomByID(room.ID)
		writeJSON(w, http.StatusOK, updated)
		return
	}
	// 置顶前确认消息存在且属于本房间、未被删除
	m, err := s.store.Get(body.MessageID)
	if err != nil || m.RoomID != room.ID || m.Deleted {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "消息不存在或不属于本房间"})
		return
	}
	if err := s.store.PinMessage(room.ID, body.MessageID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "置顶失败"})
		return
	}
	updated, _ := s.store.GetRoomByID(room.ID)
	writeJSON(w, http.StatusOK, updated)
}

// ---------- 消息 ----------

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	room := getRoom(r)
	q := r.URL.Query()
	limit := clampInt(atoi(q.Get("limit")), 1, 500, 200)
	after := atoi64(q.Get("after"))
	before := atoi64(q.Get("before"))
	if after > 0 && before > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "after 和 before 不能同时传"})
		return
	}
	msgs, hasMore, err := s.store.List(room.ID, after, before, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取消息失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "hasMore": hasMore})
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	room := getRoom(r)
	var body struct{ Content string `json:"content"` }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求格式错误"})
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "内容不能为空"})
		return
	}
	if len([]rune(content)) > s.cfg.MaxTextLen {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "文字太长了"})
		return
	}
	msg, err := s.store.InsertText(room.ID, content)
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
	_, err = s.store.SoftDelete(id)
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "消息不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "删除失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 回收站 ----------

func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	room := getRoom(r)
	msgs, err := s.store.ListTrash(room.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取回收站失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *Server) handleRestoreMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "非法 id"})
		return
	}
	msg, err := s.store.Restore(id)
	if err == store.ErrNotFound {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "消息不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "恢复失败"})
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

func (s *Server) handlePermanentDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "非法 id"})
		return
	}
	msg, err := s.store.PermanentDelete(id)
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

func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	room := getRoom(r)
	msgs, err := s.store.EmptyTrash(room.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "清空回收站失败"})
		return
	}
	var freed int64
	for _, m := range msgs {
		freed += m.FileSize
		s.removeMessageFile(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": len(msgs), "freedBytes": freed})
}

// ---------- 批量清理 ----------

func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	room := getRoom(r)
	var body struct{ Days int `json:"days"` }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.Days <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请指定要清理多少天前的消息"})
		return
	}
	ts := time.Now().AddDate(0, 0, -body.Days).UnixMilli()
	msgs, err := s.store.DeleteBefore(room.ID, ts)
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
	room := getRoom(r)
	msgs, err := s.store.DeleteAll(room.ID)
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
	room := getRoom(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"messages": []store.Message{}})
		return
	}
	msgs, err := s.store.Search(room.ID, q, 200)
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
	room := getRoom(r)
	count, fileBytes, trashCount, err := s.store.Stats(room.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "读取统计失败"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count": count, "fileBytes": fileBytes, "trashCount": trashCount, "room": room,
	})
}

func (s *Server) handleBackupNow(w http.ResponseWriter, r *http.Request) {
	name, size, err := s.backups.Run()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "备份失败: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "size": size})
}

// ---------- 外部 Webhook（BUG反馈房间） ----------

func (s *Server) handleWebhookMessage(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.WebhookToken)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "token 无效"})
		return
	}
	var body struct{ Content string `json:"content"` }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求格式错误"})
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "content 不能为空"})
		return
	}
	// 固定写入 BUG反馈 房间（ID=1）
	msg, err := s.store.InsertText(1, content)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存失败"})
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

func (s *Server) handleWebhookUpload(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	token = strings.TrimPrefix(token, "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.WebhookToken)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "token 无效"})
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "不是有效的文件上传请求"})
		return
	}
	var created []store.Message
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "读取上传数据失败"})
			return
		}
		if part.FormName() != "file" {
			part.Close()
			continue
		}
		// 固定写入 BUG反馈 房间（ID=1）
		msg, err := s.saveUploadToRoom(1, part)
		part.Close()
		if err != nil {
			if he, ok := err.(*httpError); ok {
				writeJSON(w, he.status, map[string]any{"error": he.msg})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存文件失败"})
			}
			return
		}
		created = append(created, *msg)
	}
	if len(created) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "没有收到文件"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": created})
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
			http.ServeFileFS(w, r, dist, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// ---------- 中间件 ----------

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

// ---------- 工具 ----------

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

var _ = os.Stat
