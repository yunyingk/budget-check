package ekb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Dimension 维度信息
type Dimension struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId"`
}

type dimCacheEntry struct {
	dim     *Dimension
	expires time.Time
}

// Client 合思 API 公共客户端，负责 token 管理和 HTTP 请求
type Client struct {
	Host   string
	AppKey string
	Secret string

	token  string
	expiry time.Time
	client *http.Client

	dimCache map[string]*dimCacheEntry
	dimMu    sync.RWMutex
}

func NewClient(host, appKey, secret string) *Client {
	return &Client{
		Host:     host,
		AppKey:   appKey,
		Secret:   secret,
		client:   &http.Client{Timeout: 15 * time.Second},
		dimCache: make(map[string]*dimCacheEntry),
	}
}

// GetToken 获取 accessToken，自动缓存，过期前 10 分钟刷新
func (c *Client) GetToken() (string, error) {
	if c.token != "" && time.Now().Before(c.expiry) {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{"appKey": c.AppKey, "appSecurity": c.Secret})
	resp, err := c.client.Post(c.Host+"/api/openapi/v1/auth/getAccessToken", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析token响应失败: %w", err)
	}
	val, _ := result["value"].(map[string]interface{})
	token, _ := val["accessToken"].(string)
	if token == "" {
		return "", fmt.Errorf("获取accessToken失败: %v", result)
	}
	c.token = token
	c.expiry = time.Now().Add(110 * time.Minute)
	return token, nil
}

// Get 发起 GET 请求，自动附加 accessToken 参数
func (c *Client) Get(rawURL string) (*http.Response, error) {
	token, err := c.GetToken()
	if err != nil {
		return nil, err
	}
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return c.client.Get(rawURL + sep + "accessToken=" + url.QueryEscape(token))
}

// GetWithToken 使用指定 token 发起 GET 请求
func (c *Client) GetWithToken(rawURL, token string) (*http.Response, error) {
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return c.client.Get(rawURL + sep + "accessToken=" + url.QueryEscape(token))
}

// Post 发起 POST 请求，自动附加 accessToken 参数
func (c *Client) Post(rawURL string, body []byte) (*http.Response, error) {
	token, err := c.GetToken()
	if err != nil {
		return nil, err
	}
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	req, err := http.NewRequest(http.MethodPost, rawURL+sep+"accessToken="+url.QueryEscape(token), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.client.Do(req)
}

// HostURL 拼接主机地址 + 路径
func (c *Client) HostURL(path string) string {
	return c.Host + path
}

// GetDimension 获取维度信息（带缓存，30分钟过期）
func (c *Client) GetDimension(id string) (*Dimension, error) {
	c.dimMu.RLock()
	if entry, ok := c.dimCache[id]; ok && time.Now().Before(entry.expires) {
		c.dimMu.RUnlock()
		return entry.dim, nil
	}
	c.dimMu.RUnlock()

	// 清理过期缓存
	c.cleanExpiredCache()

	token, err := c.GetToken()
	if err != nil {
		return nil, err
	}

	u := c.HostURL("/api/openapi/v1/dimensions/getDimensionById?id=" + url.QueryEscape(id))
	resp, err := c.GetWithToken(u, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析维度响应失败: %w", err)
	}

	val, _ := result["value"].(map[string]interface{})
	if val == nil {
		return nil, fmt.Errorf("维度 %s 未找到", id)
	}

	dim := &Dimension{
		ID:       id,
		Name:     getString(val, "name"),
		ParentID: getString(val, "parentId"),
	}

	c.dimMu.Lock()
	c.dimCache[id] = &dimCacheEntry{dim: dim, expires: time.Now().Add(30 * time.Minute)}
	c.dimMu.Unlock()

	return dim, nil
}

func (c *Client) cleanExpiredCache() {
	c.dimMu.Lock()
	defer c.dimMu.Unlock()
	now := time.Now()
	for k, v := range c.dimCache {
		if now.After(v.expires) {
			delete(c.dimCache, k)
		}
	}
}

// FindAncestorInTree 向上查找祖先节点是否在树中
// 返回：找到的节点 ID、是否找到、错误
func (c *Client) FindAncestorInTree(id string, tree map[string]bool, maxLevels int) (string, bool) {
	current := id
	for i := 0; i < maxLevels; i++ {
		if tree[current] {
			return current, true
		}
		dim, err := c.GetDimension(current)
		if err != nil || dim.ParentID == "" {
			return "", false
		}
		current = dim.ParentID
	}
	return "", false
}

func getString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
