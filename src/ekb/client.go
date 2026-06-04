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

	dimCache    map[string]*dimCacheEntry
	dimMu       sync.RWMutex
	feeTypes    map[string]*Dimension // feeTypeId -> Dimension (含 ParentID)
	feeTypesMu  sync.RWMutex
}

func NewClient(host, appKey, secret string) *Client {
	return &Client{
		Host:     host,
		AppKey:   appKey,
		Secret:   secret,
		client:   &http.Client{Timeout: 15 * time.Second},
		feeTypes: make(map[string]*Dimension),
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

// GetProjectDimension 获取档案维度信息（带缓存，30分钟过期）
// 仅支持 PROJECT（档案）类型，调用 /api/openapi/v1/dimensions/getDimensionById
func (c *Client) GetProjectDimension(id string) (*Dimension, error) {
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

// FindProjectAncestorInTree 向上查找祖先节点是否在树中
// 仅支持 PROJECT（档案）类型，通过 getDimensionById API 获取父级
// 返回：找到的节点 ID、是否找到
func (c *Client) FindProjectAncestorInTree(id string, tree map[string]bool, maxLevels int) (string, bool) {
	current := id
	for i := 0; i < maxLevels; i++ {
		if tree[current] {
			return current, true
		}
		dim, err := c.GetProjectDimension(current)
		if err != nil || dim.ParentID == "" {
			return "", false
		}
		current = dim.ParentID
	}
	return "", false
}

// GetDepartment 获取部门信息（带缓存，30分钟过期）
// 调用 /api/openapi/v1/departments/$idOrCode
func (c *Client) GetDepartment(id string) (*Dimension, error) {
	c.dimMu.RLock()
	if entry, ok := c.dimCache[id]; ok && time.Now().Before(entry.expires) {
		c.dimMu.RUnlock()
		return entry.dim, nil
	}
	c.dimMu.RUnlock()

	c.cleanExpiredCache()

	token, err := c.GetToken()
	if err != nil {
		return nil, err
	}

	u := c.HostURL("/api/openapi/v1/departments/$" + url.QueryEscape(id) + "?departmentBy=id")
	resp, err := c.GetWithToken(u, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析部门响应失败: %w", err)
	}

	val, _ := result["value"].(map[string]interface{})
	if val == nil {
		return nil, fmt.Errorf("部门 %s 未找到", id)
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

// FindDepartmentAncestorInTree 向上查找部门祖先节点是否在树中
// 仅支持 DEPART（部门）类型，通过 departments API 获取父级
// 返回：找到的节点 ID、是否找到
func (c *Client) FindDepartmentAncestorInTree(id string, tree map[string]bool, maxLevels int) (string, bool) {
	current := id
	for i := 0; i < maxLevels; i++ {
		if tree[current] {
			return current, true
		}
		dim, err := c.GetDepartment(current)
		if err != nil || dim.ParentID == "" {
			return "", false
		}
		current = dim.ParentID
	}
	return "", false
}

// SyncFeeTypes 全量同步费用类型到内存缓存
func (c *Client) SyncFeeTypes() error {
	token, err := c.GetToken()
	if err != nil {
		return err
	}

	u := c.HostURL("/api/openapi/v1/feeTypes")
	resp, err := c.GetWithToken(u, token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("解析费用类型响应失败: %w", err)
	}

	items, ok := result["items"].([]interface{})
	if !ok {
		return fmt.Errorf("费用类型响应格式错误: 缺少 items 字段")
	}

	newFeeTypes := make(map[string]*Dimension, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		newFeeTypes[id] = &Dimension{
			ID:       id,
			Name:     getString(m, "name"),
			ParentID: getString(m, "parentId"),
		}
	}

	c.feeTypesMu.Lock()
	c.feeTypes = newFeeTypes
	c.feeTypesMu.Unlock()

	return nil
}

// FindFeeTypeAncestorInTree 向上查找费用类型祖先节点是否在树中
// 仅支持 FEE_TYPE（消费类型）类型，从内存缓存中查找父级
// 返回：找到的节点 ID、是否找到
func (c *Client) FindFeeTypeAncestorInTree(id string, tree map[string]bool, maxLevels int) (string, bool) {
	c.feeTypesMu.RLock()
	defer c.feeTypesMu.RUnlock()

	current := id
	for i := 0; i < maxLevels; i++ {
		if tree[current] {
			return current, true
		}
		ft, ok := c.feeTypes[current]
		if !ok || ft.ParentID == "" {
			return "", false
		}
		current = ft.ParentID
	}
	return "", false
}

// FeeTypeCount 返回已缓存的费用类型数量
func (c *Client) FeeTypeCount() int {
	c.feeTypesMu.RLock()
	defer c.feeTypesMu.RUnlock()
	return len(c.feeTypes)
}

func getString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}
