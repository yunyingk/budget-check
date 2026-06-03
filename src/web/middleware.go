package web

import "net/http"

func authMiddleware(tokenStore *TokenStore, password string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if password == "" {
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
}

func isAPIRequest(r *http.Request) bool {
	return len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/"
}
