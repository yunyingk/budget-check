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

// Client 合思 API 公共客户端，负责 token 管理和 HTTP 请求
type Client struct {
	Host   string
	AppKey string
	Secret string

	token  string
	expiry time.Time
	client *http.Client

	dimCache map[string]*Dimension
	dimMu    sync.RWMutex
}

func NewClient(host, appKey, secret string) *Client {
	return &Client{
		Host:     host,
		AppKey:   appKey,
		Secret:   secret,
		client:   &http.Client{Timeout: 15 * time.Second},
		dimCache: make(map[string]*Dimension),
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
	json.NewDecoder(resp.Body).Decode(&result)
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

// HostURL 拼接主机地址 + 路径
func (c *Client) HostURL(path string) string {
	return c.Host + path
}

// GetDimension 获取维度信息（带缓存）
func (c *Client) GetDimension(id string) (*Dimension, error) {
	c.dimMu.RLock()
	if dim, ok := c.dimCache[id]; ok {
		c.dimMu.RUnlock()
		return dim, nil
	}
	c.dimMu.RUnlock()

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
	json.NewDecoder(resp.Body).Decode(&result)

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
	c.dimCache[id] = dim
	c.dimMu.Unlock()

	return dim, nil
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
