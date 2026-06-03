package web

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const tokenMaxAge = 2 * time.Hour

// TokenStore 内存 token 存储（带过期清理）
type TokenStore struct {
	mu     sync.RWMutex
	tokens map[string]time.Time
}

// NewTokenStore 创建 token 存储并启动后台清理 goroutine
func NewTokenStore() *TokenStore {
	ts := &TokenStore{tokens: make(map[string]time.Time)}
	go ts.cleanup()
	return ts
}

// Generate 生成新 token 并记录过期时间
func (s *TokenStore) Generate() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.tokens[token] = time.Now().Add(tokenMaxAge)
	s.mu.Unlock()
	return token
}

// Valid 检查 token 是否有效（存在且未过期）
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

// Revoke 使 token 失效
func (s *TokenStore) Revoke(token string) {
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

// cleanup 后台定期清理过期 token
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
