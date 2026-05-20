package ekb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Client 合思 API 公共客户端，负责 token 管理和 HTTP 请求
type Client struct {
	Host   string
	AppKey string
	Secret string

	token  string
	expiry time.Time
	client *http.Client
}

func NewClient(host, appKey, secret string) *Client {
	return &Client{
		Host:   host,
		AppKey: appKey,
		Secret: secret,
		client: &http.Client{Timeout: 15 * time.Second},
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
	return c.client.Get(rawURL + "?accessToken=" + url.QueryEscape(token))
}

// GetWithToken 使用指定 token 发起 GET 请求（用于已持有 token 的场景）
func (c *Client) GetWithToken(rawURL, token string) (*http.Response, error) {
	return c.client.Get(rawURL + "?accessToken=" + url.QueryEscape(token))
}

// HostURL 拼接主机地址 + 路径
func (c *Client) HostURL(path string) string {
	return c.Host + path
}
