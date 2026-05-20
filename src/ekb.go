package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type EkbClient struct {
	host      string
	basePath  string
	appKey    string
	secret    string
	token     string
	expiry    time.Time
	client    *http.Client
}

func NewEkbClient(cfg *Config) *EkbClient {
	return &EkbClient{
		host:     cfg.Ekb.Host,
		basePath: "/api/openapi",
		appKey:   cfg.Ekb.AppKey,
		secret:   cfg.Ekb.AppSecret,
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *EkbClient) GetToken() (string, error) {
	if c.token != "" && time.Now().Before(c.expiry) {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{
		"appKey":      c.appKey,
		"appSecurity": c.secret,
	})
	resp, err := c.client.Post(c.apiURL("/v1/auth/getAccessToken"), "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	val, _ := result["value"].(map[string]interface{})
	token, _ := val["accessToken"].(string)
	c.token = token
	c.expiry = time.Now().Add(110 * time.Minute)
	return token, nil
}

type TicketInfo struct {
	DeptCode    string
	ArchiveCode string
	ProjectCode string
	ExpenseType string
	RawNatureID string
}

func (c *EkbClient) FetchTicket(ticketCode string, natureMap map[string]string) (*TicketInfo, error) {
	token, err := c.GetToken()
	if err != nil {
		return nil, err
	}

	resp, err := c.get("/v1.1/flowDetails/byCode", url.Values{
		"accessToken": {token},
		"code":        {ticketCode},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	val, _ := result["value"].(map[string]interface{})
	form, _ := val["form"].(map[string]interface{})
	if form == nil {
		return nil, fmt.Errorf("未找到单据详情")
	}

	deptCode, _ := form["E_system_costcenter"].(string)

	archiveCode := ""
	if details, ok := form["details"].([]interface{}); ok && len(details) > 0 {
		if d, ok := details[0].(map[string]interface{}); ok {
			if ftf, ok := d["feeTypeForm"].(map[string]interface{}); ok {
				archiveCode, _ = ftf["u_费用类型档案"].(string)
			}
		}
	}
	if archiveCode == "" {
		archiveCode, _ = form["u_费用类型档案"].(string)
	}

	projectCode, _ := form["项目"].(string)
	natureID, _ := form["u_费用性质"].(string)
	expenseType := natureMap[natureID]
	if expenseType == "" {
		expenseType = "未知"
	}

	return &TicketInfo{
		DeptCode:    deptCode,
		ArchiveCode: archiveCode,
		ProjectCode: projectCode,
		ExpenseType:  expenseType,
		RawNatureID: natureID,
	}, nil
}

func (c *EkbClient) GetParentID(dimValueID string) (string, error) {
	token, err := c.GetToken()
	if err != nil {
		return "", err
	}
	resp, err := c.get("/v1/dimensions/getDimensionById", url.Values{
		"accessToken": {token},
		"id":          {dimValueID},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	val, _ := result["value"].(map[string]interface{})
	pid, _ := val["parentId"].(string)
	return pid, nil
}

func (c *EkbClient) GetDimInfo(dimID string) (map[string]interface{}, error) {
	token, err := c.GetToken()
	if err != nil {
		return nil, err
	}
	resp, err := c.get("/v1/dimensions/getDimensionById", url.Values{
		"accessToken": {token},
		"id":          {dimID},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	val, _ := result["value"].(map[string]interface{})
	return val, nil
}

func (c *EkbClient) get(path string, params url.Values) (*http.Response, error) {
	u := c.apiURL(path) + "?" + params.Encode()
	return c.client.Get(u)
}

func (c *EkbClient) apiURL(path string) string {
	return c.host + c.basePath + path
}