package wake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"quota-schedule-refresh/internal/host"
)

const CodexActivationURL = "https://chatgpt.com/backend-api/codex/responses"
const usageLimitReachedType = "usage_limit_reached"

type AuthMaterial struct {
	AccessToken string
	AccountID   string
}

func ParseAuthMaterial(raw []byte) (AuthMaterial, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return AuthMaterial{}, fmt.Errorf("凭证内容为空")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return AuthMaterial{}, fmt.Errorf("凭证不是合法数据")
	}
	material := AuthMaterial{
		AccessToken: firstStringField(root, "access_token", "accessToken", "api_key", "apiKey", "token"),
		AccountID:   firstStringField(root, "account_id", "chatgpt_account_id", "accountId"),
	}
	if tokensRaw, ok := root["tokens"]; ok {
		var tokens map[string]json.RawMessage
		if json.Unmarshal(tokensRaw, &tokens) == nil {
			if material.AccessToken == "" {
				material.AccessToken = firstStringField(tokens, "access_token", "accessToken", "api_key", "token")
			}
			if material.AccountID == "" {
				material.AccountID = firstStringField(tokens, "account_id", "accountId")
			}
		}
	}
	if strings.TrimSpace(material.AccessToken) == "" {
		return AuthMaterial{}, fmt.Errorf("缺少访问令牌")
	}
	return material, nil
}

func firstStringField(object map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw, ok := object[key]
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			continue
		}
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func BuildCodexRequest(material AuthMaterial, model, prompt string) (host.HTTPRequest, error) {
	if strings.TrimSpace(material.AccessToken) == "" {
		return host.HTTPRequest{}, fmt.Errorf("缺少访问令牌")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return host.HTTPRequest{}, fmt.Errorf("缺少模型名称")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "hello"
	}
	body, err := json.Marshal(map[string]any{
		"model":        model,
		"instructions": "You are a helpful assistant.",
		"input": []map[string]any{{
			"type": "message",
			"role": "user",
			"content": []map[string]any{{
				"type": "input_text",
				"text": prompt,
			}},
		}},
		"store":  false,
		"stream": true,
	})
	if err != nil {
		return host.HTTPRequest{}, err
	}
	headers := host.Header{
		"Accept":        {"text/event-stream"},
		"Authorization": {"Bearer " + material.AccessToken},
		"Content-Type":  {"application/json"},
		"OpenAI-Beta":   {"responses=v1"},
		"originator":    {"codex_cli_rs"},
		"User-Agent":    {"codex_cli_rs/0.76.0"},
	}
	if accountID := strings.TrimSpace(material.AccountID); accountID != "" {
		headers["Chatgpt-Account-Id"] = []string{accountID}
	}
	return host.HTTPRequest{Method: http.MethodPost, URL: CodexActivationURL, Headers: headers, Body: body}, nil
}

func EvaluateCodexSuccess(statusCode int, body []byte) (bool, string) {
	if !host.IsHTTPSuccess(statusCode) {
		return false, fmt.Sprintf("Codex唤醒失败：上游返回非成功状态（HTTP %d）", statusCode)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return false, "Codex唤醒失败：响应体为空"
	}
	if strings.Contains(trimmed, "event:") || strings.Contains(trimmed, "data:") {
		return evaluateCodexSSE(trimmed)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return false, "Codex唤醒失败：响应不是合法数据"
	}
	if msg, hasErr := codexError(root); hasErr {
		return false, msg
	}
	if codexHasSuccess(root) {
		return true, ""
	}
	return false, "Codex唤醒失败：响应缺少有效输出结构"
}

func evaluateCodexSSE(sse string) (bool, string) {
	if strings.Contains(sse, `"type":"response.completed"`) ||
		(strings.Contains(sse, `"type":"response.created"`) && strings.Contains(sse, `"id":"resp_`)) {
		return true, ""
	}
	if strings.Contains(sse, "usage_limit") {
		return false, "Codex唤醒失败：用量额度已耗尽"
	}
	if strings.Contains(sse, `"type":"response.failed"`) || strings.Contains(sse, `"type":"error"`) {
		return false, "Codex唤醒失败：上游返回业务错误"
	}
	return false, "Codex唤醒失败：响应缺少有效输出结构"
}

func codexHasSuccess(root map[string]json.RawMessage) bool {
	if raw, ok := root["id"]; ok {
		var id string
		if json.Unmarshal(raw, &id) == nil && strings.TrimSpace(id) != "" {
			return true
		}
	}
	if raw, ok := root["output"]; ok {
		var output []json.RawMessage
		if json.Unmarshal(raw, &output) == nil && len(output) > 0 {
			return true
		}
	}
	return false
}

func codexError(root map[string]json.RawMessage) (string, bool) {
	raw, ok := root["error"]
	if !ok || string(raw) == "null" {
		return "", false
	}
	text := strings.ToLower(string(raw))
	if strings.Contains(text, usageLimitReachedType) || strings.Contains(text, "usage limit") {
		return "Codex唤醒失败：用量额度已耗尽", true
	}
	return "Codex唤醒失败：上游返回业务错误", true
}
