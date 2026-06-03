package web

import (
	"encoding/json"
	"net/http"
)

func handleLogin(w http.ResponseWriter, r *http.Request, tokenStore *TokenStore, password string) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if password == "" {
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
	if body.Password != password {
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

func handleLogout(w http.ResponseWriter, r *http.Request, tokenStore *TokenStore) {
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

func handleLoginPage(w http.ResponseWriter, r *http.Request, password string) {
	if r.URL.Path != "/login" {
		http.NotFound(w, r)
		return
	}
	if password == "" {
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
