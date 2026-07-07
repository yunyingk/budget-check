package ekb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetTokenSingleflight 验证 token 过期时，N 个并发 goroutine 只触发一次刷新。
// 这是 P0-1 的核心保障：合思宕机/抖动时不会 N 路并发打 auth 接口。
func TestGetTokenSingleflight(t *testing.T) {
	var authCalls int32 = 0

	// 模拟合思 auth 端点：每次调用 sleep 一会放大竞争窗口，统计调用次数
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&authCalls, 1)
		time.Sleep(100 * time.Millisecond) // 模拟慢响应，让其他 goroutine 有机会涌入
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"value": map[string]interface{}{"accessToken": "tok-xyz"},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "k", "s")

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, _ = client.GetTokenContext(t.Context())
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&authCalls); got != 1 {
		t.Fatalf("并发 %d 个 GetTokenContext 应只触发 1 次 auth 调用，实际 %d 次", n, got)
	}
}

// TestGetTokenCachesUntilExpiry 验证 token 未过期时直接用缓存，不重新请求
func TestGetTokenCachesUntilExpiry(t *testing.T) {
	var authCalls int32 = 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&authCalls, 1)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"value": map[string]interface{}{"accessToken": "tok-abc"},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "k", "s")

	for i := 0; i < 5; i++ {
		tok, err := client.GetTokenContext(t.Context())
		if err != nil {
			t.Fatalf("第 %d 次获取 token 失败: %v", i, err)
		}
		if tok != "tok-abc" {
			t.Fatalf("token 不匹配: got %s", tok)
		}
	}
	if got := atomic.LoadInt32(&authCalls); got != 1 {
		t.Fatalf("5 次获取应只调 1 次 auth（走缓存），实际 %d 次", got)
	}
}

// TestConsecutiveFails 健康度计数：失败累加，成功归零
func TestConsecutiveFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// auth 总是成功
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"value": map[string]interface{}{"accessToken": "tok"},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "k", "s")

	if got := client.ConsecutiveFails(); got != 0 {
		t.Fatalf("初始失败计数应为 0，got %d", got)
	}

	client.recordFailure()
	client.recordFailure()
	if got := client.ConsecutiveFails(); got != 2 {
		t.Fatalf("2 次失败后应为 2，got %d", got)
	}

	client.recordSuccess()
	if got := client.ConsecutiveFails(); got != 0 {
		t.Fatalf("成功后应归零，got %d", got)
	}
}

// TestGetTokenErrorDoesNotCorruptState 刷新失败时 refreshing 正确复位，
// 下次调用不会因为 refreshing 卡死而永远拿不到 token
func TestGetTokenErrorDoesNotCorruptState(t *testing.T) {
	callCount := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			// 第一次返回坏的响应，触发刷新失败
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, "boom")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"value": map[string]interface{}{"accessToken": "tok-recovered"},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "k", "s")

	// 第一次失败
	_, err := client.GetTokenContext(t.Context())
	if err == nil {
		t.Fatal("第一次（坏响应）应返回错误")
	}

	// 第二次应能恢复，而不是卡在 refreshing=true
	tok, err := client.GetTokenContext(t.Context())
	if err != nil {
		t.Fatalf("第二次应恢复成功，got err: %v", err)
	}
	if tok != "tok-recovered" {
		t.Fatalf("token 应为 tok-recovered，got %s", tok)
	}

	if client.refreshing {
		t.Fatal("失败后 refreshing 应复位为 false")
	}
}
