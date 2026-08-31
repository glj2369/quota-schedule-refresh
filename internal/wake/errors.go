package wake

import (
	"encoding/json"
	"html"
	"strings"
)

// 宿主逐层包装错误，最终文本形如
// "host callback host.model.execute: host_call_failed: auth_unavailable: no auth available"。
// 这些壳对使用者没有意义，展示前逐层剥掉。
var errorWrappers = []string{
	"host callback host.model.execute:",
	"host.model.execute:",
	"host_call_failed:",
	"host call failed:",
	"model_execute_failed:",
	"execute failed:",
}

// friendlyError 生成用于列表展示的文本，完整原文由 Result.Detail 保留。
func friendlyError(raw string) string {
	text := stripErrorWrappers(html.UnescapeString(raw))
	if message := jsonErrorMessage(text); message != "" {
		text = message
	}
	if text = compactSpace(text); text == "" {
		return "宿主模型执行失败"
	}
	return clipRunes(text, 300)
}

func friendlyHostError(err error) string {
	if err == nil {
		return "宿主模型执行失败"
	}
	return friendlyError(err.Error())
}

// errorDetail 保留完整原文供悬停查看，仅解码 HTML 实体并压缩空白。
func errorDetail(raw string) string {
	return clipRunes(compactSpace(html.UnescapeString(raw)), 600)
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

// jsonErrorMessage 从上游返回的错误体里取出 message，避免整段 JSON 出现在表格里。
func jsonErrorMessage(text string) string {
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}
	var payload any
	if json.Unmarshal([]byte(text[start:]), &payload) != nil {
		return ""
	}
	return extractReplyText(payload, 0)
}
