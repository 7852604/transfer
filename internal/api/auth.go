package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const tokenCookie = "transfer_token"

type loginLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

// allow 同一 IP 每分钟最多 10 次登录尝试
func (l *loginLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = make(map[string][]time.Time)
	}
	recent := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 10 {
		l.hits[ip] = recent
		return false
	}
	l.hits[ip] = append(recent, now)
	return true
}

func RandToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求格式错误"})
		return
	}
	ip := clientIP(r)
	if !s.limiter.allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "尝试太频繁，请一分钟后再试"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.cfg.Password)) != 1 {
		time.Sleep(300 * time.Millisecond) // 拖慢暴力破解
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "密码不对"})
		return
	}
	token := RandToken()
	if err := s.store.CreateToken(token); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "服务端错误"})
		return
	}
	// 10 年有效：换设备或清浏览器数据才需要重新输入
	http.SetCookie(w, &http.Cookie{
		Name:     tokenCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   86400 * 3650,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.CookieSecure,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(tokenCookie); err == nil {
		_ = s.store.DeleteToken(c.Value)
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		_ = s.store.DeleteToken(strings.TrimPrefix(h, "Bearer "))
	}
	http.SetCookie(w, &http.Cookie{Name: tokenCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// auth 校验 cookie 或 Authorization: Bearer 头，二者任一通过即可
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie(tokenCookie); err == nil {
			token = c.Value
		}
		if token == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimPrefix(h, "Bearer ")
			}
		}
		ok, err := s.store.TokenExists(token)
		if token == "" || err != nil || !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "未登录"})
			return
		}
		next(w, r)
	}
}
