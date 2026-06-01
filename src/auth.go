package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const tokenMaxAge = 2 * time.Hour

type TokenStore struct {
	mu     sync.RWMutex
	tokens map[string]time.Time
}

func NewTokenStore() *TokenStore {
	ts := &TokenStore{tokens: make(map[string]time.Time)}
	go ts.cleanup()
	return ts
}

func (s *TokenStore) Generate() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.tokens[token] = time.Now().Add(tokenMaxAge)
	s.mu.Unlock()
	return token
}

func (s *TokenStore) Valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.RLock()
	expiry, ok := s.tokens[token]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Now().Before(expiry)
}

func (s *TokenStore) Revoke(token string) {
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

func (s *TokenStore) cleanup() {
	for {
		time.Sleep(10 * time.Minute)
		s.mu.Lock()
		now := time.Now()
		for t, exp := range s.tokens {
			if now.After(exp) {
				delete(s.tokens, t)
			}
		}
		s.mu.Unlock()
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if cfg.Web.Password == "" {
		writeJSON(w, 400, map[string]string{"error": "未设置密码，登录已禁用"})
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "请求格式错误"})
		return
	}
	if body.Password != cfg.Web.Password {
		writeJSON(w, 403, map[string]string{"error": "密码错误"})
		return
	}
	token := tokenStore.Generate()
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		MaxAge:   int(tokenMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, 200, map[string]string{"message": "登录成功"})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, _ := r.Cookie("token"); c != nil {
		tokenStore.Revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "token",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	writeJSON(w, 200, map[string]string{"message": "已登出"})
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.Web.Password == "" {
			next(w, r)
			return
		}
		c, err := r.Cookie("token")
		if err != nil || !tokenStore.Valid(c.Value) {
			if isAPIRequest(r) {
				writeJSON(w, 401, map[string]string{"error": "未登录或登录已过期"})
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func isAPIRequest(r *http.Request) bool {
	return len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/"
}

func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/login" {
		http.NotFound(w, r)
		return
	}
	if cfg.Web.Password == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	data, err := staticFS.ReadFile("static/login.html")
	if err != nil {
		http.Error(w, "页面加载失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}