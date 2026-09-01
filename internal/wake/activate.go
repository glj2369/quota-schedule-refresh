package wake

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"sync"
	"time"

	"quota-schedule-refresh/internal/config"
	"quota-schedule-refresh/internal/host"
)

type Result struct {
	AuthID     string `json:"auth_id"`
	Label      string `json:"label"`
	Model      string `json:"model"`
	Status     string `json:"status"`
	Success    bool   `json:"success"`
	LastError  string `json:"last_error,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Attempts   int    `json:"attempts,omitempty"`
	Reply      string `json:"reply,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type Activator struct {
	Host             host.Client
	Config           config.Config
	PinPreferredAuth func(authID string) func()
	executeMu        sync.Mutex
}

func (a *Activator) Activate(ctx context.Context, authID, label, model string, disabled bool) Result {
	result := Result{AuthID: authID, Label: label, Model: model, Status: "failed"}
	if a == nil || a.Host == nil {
		result.LastError = "缺少宿主依赖，插件未正确加载"
		return result
	}

	if disabled && a.Config.EnableDisabled {
		enableCtx, cancel := a.attemptContext(ctx)
		err := a.enableAuth(enableCtx, authID)
		cancel()
		if err != nil {
			result.LastError = "启用已禁用凭证失败"
			result.Detail = errorDetail(err.Error())
			return result
		}
	}

	return a.wakeCPA(ctx, authID, model, result)
}

func (a *Activator) wakeCPA(ctx context.Context, authID, model string, previous Result) Result {
	attempts := a.Config.RetryCount + 1
	if attempts < 1 {
		attempts = 1
	}
	return a.withBoostedAuth(ctx, previous, func(callCtx context.Context) Result {
		var last Result
		for i := 1; i <= attempts; i++ {
			attemptCtx, cancel := a.attemptContext(callCtx)
			last = a.executeOnce(attemptCtx, model, previous)
			cancel()
			last.Attempts = i
			if last.Success {
				return last
			}
			if callCtx.Err() != nil {
				return last
			}
			if i < attempts && !sleepCtx(callCtx, a.Config.RetryInterval) {
				return last
			}
		}
		// 重试次数由「结果」列单独展示，不再拼进错误文本。
		return last
	})
}

func (a *Activator) attemptContext(parent context.Context) (context.Context, context.CancelFunc) {
	if a.Config.Timeout > 0 {
		return context.WithTimeout(parent, a.Config.Timeout)
	}
	return parent, func() {}
}

func (a *Activator) executeOnce(ctx context.Context, model string, previous Result) Result {
	if a.PinPreferredAuth != nil {
		if unpin := a.PinPreferredAuth(previous.AuthID); unpin != nil {
			defer unpin()
		}
	}
	prompt := strings.TrimSpace(a.Config.Prompt)
	if prompt == "" {
		prompt = "hello"
	}
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"input": []map[string]string{{
			"role":    "user",
			"content": prompt,
		}},
		"store": false,
	})
	response, err := a.Host.ModelExecute(ctx, host.ModelExecuteRequest{
		Model:  model,
		Stream: false,
		Body:   body,
	})
	if err != nil {
		previous.Status = "failed"
		previous.Success = false
		previous.LastError = friendlyHostError(err)
		previous.Detail = errorDetail(err.Error())
		previous.Reply = ""
		return previous
	}
	previous.HTTPStatus = response.StatusCode
	previous.Reply = summarizeModelReply(response.Body)
	if !host.IsHTTPSuccess(response.StatusCode) {
		previous.Status = "failed"
		previous.Success = false
		if previous.Reply != "" {
			previous.LastError = friendlyError(previous.Reply)
		} else {
			previous.LastError = "上游返回非成功状态"
		}
		previous.Detail = errorDetail(string(response.Body))
		previous.Reply = ""
		return previous
	}
	previous.Status = "success"
	previous.Success = true
	previous.LastError = ""
	previous.Detail = ""
	return previous
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func matchAuth(file host.AuthFile, authID string) bool {
	want := strings.TrimSpace(authID)
	for _, value := range []string{file.ID, file.Name, file.AuthIndex} {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func (a *Activator) enableAuth(ctx context.Context, authID string) error {
	files, err := a.Host.ListAuthFiles(ctx)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !matchAuth(file, authID) {
			continue
		}
		var document map[string]json.RawMessage
		if json.Unmarshal(file.Data, &document) != nil {
			continue
		}
		flag, _ := json.Marshal(false)
		document["disabled"] = flag
		encoded, err := json.Marshal(document)
		if err != nil {
			return err
		}
		return a.Host.SaveAuthFile(ctx, file.Name, encoded)
	}
	return fmt.Errorf("未找到凭证")
}

func summarizeModelReply(body []byte) string {
	payloads := parseExecutePayloads(string(body))
	var best string
	for _, raw := range payloads {
		if extracted := extractReplyText(raw, 0); extracted != "" {
			best = extracted
		}
	}
	if best != "" {
		return clipRunes(compactSpace(best), 160)
	}
	for i := len(payloads) - 1; i >= 0; i-- {
		if label := statusLabel(payloads[i]); label != "" {
			return label
		}
	}
	return ""
}

func parseExecutePayloads(raw string) []any {
	text := strings.TrimSpace(html.UnescapeString(raw))
	if text == "" {
		return nil
	}
	payloads := decodeJSONPayloads(extractSSEData(text))
	if len(payloads) == 0 {
		payloads = decodeJSONPayloads(text)
	}
	return payloads
}

func extractSSEData(text string) string {
	if !strings.Contains(text, "data:") {
		return text
	}
	var chunks []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		chunks = append(chunks, payload)
	}
	if len(chunks) == 0 {
		return text
	}
	return strings.Join(chunks, "\n")
}

func decodeJSONPayloads(text string) []any {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(text)))
	var payloads []any
	for {
		var raw any
		if err := decoder.Decode(&raw); err != nil {
			break
		}
		payloads = append(payloads, raw)
	}
	return payloads
}

func statusLabel(value any) string {
	item, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if nested, ok := item["response"].(map[string]any); ok {
		if status, _ := nested["status"].(string); strings.TrimSpace(status) != "" {
			return strings.TrimSpace(status)
		}
	}
	if status, _ := item["status"].(string); strings.TrimSpace(status) != "" {
		return strings.TrimSpace(status)
	}
	return ""
}

func extractReplyText(value any, depth int) string {
	if depth > 8 || value == nil {
		return ""
	}
	switch item := value.(type) {
	case string:
		text := strings.TrimSpace(html.UnescapeString(item))
		if text == "" {
			return ""
		}
		if looksLikeJSON(text) {
			var nested any
			if json.Unmarshal([]byte(text), &nested) == nil {
				if extracted := extractReplyText(nested, depth+1); extracted != "" {
					return extracted
				}
			}
			return ""
		}
		if isNoiseText(text) {
			return ""
		}
		return text
	case []any:
		parts := make([]string, 0, len(item))
		for _, entry := range item {
			if text := extractReplyText(entry, depth+1); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	case map[string]any:
		for _, key := range []string{"output_text", "text", "delta", "refusal"} {
			if text := extractReplyText(item[key], depth+1); text != "" {
				return text
			}
		}
		if message, ok := item["message"]; ok {
			if text := extractReplyText(message, depth+1); text != "" {
				return text
			}
		}
		if errValue, ok := item["error"]; ok {
			if text := extractReplyText(errValue, depth+1); text != "" {
				return text
			}
		}
		for _, key := range []string{"content", "output", "choices", "response"} {
			if text := extractReplyText(item[key], depth+1); text != "" {
				return text
			}
		}
	}
	return ""
}

func looksLikeJSON(text string) bool {
	return strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")
}

func isNoiseText(text string) bool {
	if strings.HasPrefix(text, "resp_") || strings.HasPrefix(text, "msg_") {
		return true
	}
	return strings.Contains(text, `"type":"response.`)
}

func compactSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func clipRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
