package wake

import (
	"strings"
)

// 宿主逐层包装错误，最终文本形如
// "host callback host.model.execute: host_call_failed: auth_unavailable: no auth available (providers=codex, model=gpt-5.2-codex)"。
// 这些壳对使用者没有意义，展示前逐层剥掉。
var errorWrappers = []string{
	"host callback host.model.execute:",
	"host.model.execute:",
	"host_call_failed:",
	"host call failed:",
	"model_execute_failed:",
	"execute failed:",
}

// errorHints 按从具体到宽泛的顺序匹配，命中即用中文短句替换整条消息。
var errorHints = []struct {
	keywords []string
	message  string
}{
	{[]string{"auth_unavailable", "no auth available"}, "CPA 没有可用凭证，可能已被禁用或全部限流"},
	{[]string{"context deadline exceeded", "timed out", "timeout"}, "请求超时"},
	{[]string{"context canceled", "request canceled"}, "请求被取消"},
	{[]string{"connection refused", "no such host", "dial tcp"}, "无法连接 CPA 接口"},
	{[]string{"model_not_found", "model not found", "unknown model"}, "模型不可用"},
	{[]string{"unauthorized", "invalid_api_key", "invalid token"}, "凭证认证失败，需要重新登录"},
	{[]string{"too many requests", "rate_limit", "rate limit"}, "触发限流，稍后重试"},
	{[]string{"insufficient_quota", "quota_exceeded", "usage limit"}, "额度不足"},
	{[]string{"no auth", "auth not found"}, "找不到可用凭证"},
}

// friendlyError 生成用于列表展示的短消息，原始文本由 Result.Detail 保留。
func friendlyError(raw string) string {
	text := stripErrorWrappers(raw)
	if text == "" {
		return "宿主模型执行失败"
	}
	lower := strings.ToLower(text)
	for _, hint := range errorHints {
		for _, keyword := range hint.keywords {
			if strings.Contains(lower, keyword) {
				return hint.message
			}
		}
	}
	return clipRunes(compactSpace(stripTrailingContext(text)), 120)
}

func friendlyHostError(err error) string {
	if err == nil {
		return "宿主模型执行失败"
	}
	return friendlyError(err.Error())
}

func stripErrorWrappers(raw string) string {
	text := compactSpace(raw)
	for changed := true; changed; {
		changed = false
		for _, wrapper := range errorWrappers {
			if len(text) >= len(wrapper) && strings.EqualFold(text[:len(wrapper)], wrapper) {
				text = strings.TrimSpace(text[len(wrapper):])
				changed = true
			}
		}
	}
	return text
}

// stripTrailingContext 去掉结尾的 "(providers=..., model=...)"，
// 这些字段在表格里已有独立列。
func stripTrailingContext(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasSuffix(trimmed, ")") {
		return trimmed
	}
	open := strings.LastIndex(trimmed, "(")
	if open <= 0 {
		return trimmed
	}
	inner := strings.ToLower(trimmed[open+1 : len(trimmed)-1])
	if !strings.Contains(inner, "=") {
		return trimmed
	}
	return strings.TrimSpace(trimmed[:open])
}
