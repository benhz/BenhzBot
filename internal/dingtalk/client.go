package dingtalk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	AppKey      string
	AppSecret   string
	AgentID     string
	RobotCode   string
	AccessToken string
	TokenExpiry time.Time
}

func NewClient(appKey, appSecret, agentID, robotCode string) *Client {
	return &Client{
		AppKey:    appKey,
		AppSecret: appSecret,
		AgentID:   agentID,
		RobotCode: robotCode,
	}
}

// 获取 Access Token
func (c *Client) GetAccessToken() (string, error) {
	// 如果 token 未过期，直接返回
	if c.AccessToken != "" && time.Now().Before(c.TokenExpiry) {
		return c.AccessToken, nil
	}

	url := fmt.Sprintf(
		"https://oapi.dingtalk.com/gettoken?appkey=%s&appsecret=%s",
		c.AppKey, c.AppSecret,
	)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("请求 access_token 失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 token 响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("获取 token 失败: %s", result.ErrMsg)
	}

	c.AccessToken = result.AccessToken
	c.TokenExpiry = time.Now().Add(time.Duration(result.ExpiresIn-300) * time.Second)

	return c.AccessToken, nil
}

// 发送群消息（文本）- 使用新版 API
func (c *Client) SendGroupMessage(chatID, content string) error {
	token, err := c.GetAccessToken()
	if err != nil {
		return err
	}

	url := "https://api.dingtalk.com/v1.0/robot/groupMessages/send"

	// 构造消息参数
	msgParam := map[string]string{
		"content": content,
	}
	msgParamJSON, err := json.Marshal(msgParam)
	if err != nil {
		return fmt.Errorf("序列化消息参数失败: %w", err)
	}

	payload := map[string]interface{}{
		"msgKey":            "sampleText",
		"msgParam":          string(msgParamJSON),
		"openConversationId": chatID,
		"robotCode":         c.RobotCode,
	}

	return c.sendRequestWithHeader(url, payload, token)
}

// 发送 ActionCard
func (c *Client) SendActionCard(chatID, title, text string, buttons []ActionButton) error {
	token, err := c.GetAccessToken()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://oapi.dingtalk.com/chat/send?access_token=%s", token)

	btnList := make([]map[string]string, len(buttons))
	for i, btn := range buttons {
		btnList[i] = map[string]string{
			"title":     btn.Title,
			"action_url": btn.ActionURL,
		}
	}

	payload := map[string]interface{}{
		"chatid":  chatID,
		"msgtype": "action_card",
		"action_card": map[string]interface{}{
			"title":              title,
			"text":               text,
			"btn_orientation":    "0",
			"btn_json_list":      btnList,
		},
	}

	return c.sendRequest(url, payload)
}

// 发送 Markdown 消息
func (c *Client) SendMarkdown(chatID, title, text string) error {
	return c.SendMarkdownWithMentions(chatID, title, text, nil)
}

// 发送 Markdown 消息并@指定用户
func (c *Client) SendMarkdownWithMentions(chatID, title, text string, atUserIDs []string) error {
	token, err := c.GetAccessToken()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://oapi.dingtalk.com/chat/send?access_token=%s", token)

	// 如果有@用户，在文本末尾添加@提及
	if len(atUserIDs) > 0 {
		text = text + "\n\n"
		for _, userID := range atUserIDs {
			text = text + fmt.Sprintf("@%s ", userID)
		}
	}

	payload := map[string]interface{}{
		"chatid":  chatID,
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  text,
		},
	}

	return c.sendRequest(url, payload)
}

// 通用发送请求（旧版 API，带 access_token 在 URL 中）
func (c *Client) sendRequest(url string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("钉钉 API 错误 (%d): %s", result.ErrCode, result.ErrMsg)
	}

	return nil
}

// 新版 API 请求（access_token 在 header 中）
func (c *Client) sendRequestWithHeader(url string, payload interface{}, token string) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// 新版 API 返回格式可能不同，先检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("钉钉 API 请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 尝试解析响应
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析响应失败: %w, 响应: %s", err, string(body))
	}

	// 检查是否有错误信息
	if errCode, ok := result["errcode"].(float64); ok && errCode != 0 {
		errMsg := result["errmsg"].(string)
		return fmt.Errorf("钉钉 API 错误 (%d): %s", int(errCode), errMsg)
	}

	return nil
}

type ActionButton struct {
	Title     string
	ActionURL string
}
