package wake

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"quota-schedule-refresh/internal/host"
)

const priorityBoostFloor = 1000

const restoreFailedMessage = "优先级回写失败"

type prioritySnapshot struct {
	present bool
	value   int
}

type boostSession struct {
	file     host.AuthFile
	name     string
	original prioritySnapshot
}

func (a *Activator) withBoostedAuth(ctx context.Context, previous Result, fn func(context.Context) Result) Result {
	a.executeMu.Lock()
	defer a.executeMu.Unlock()

	session, err := a.prepareBoost(ctx, previous.AuthID)
	if err != nil {
		previous.Status = "failed"
		previous.Success = false
		previous.LastError = err.Error()
		previous.Detail = errorDetail(err.Error())
		previous.Reply = ""
		return previous
	}
	result := fn(ctx)
	if session == nil {
		return result
	}
	if restoreErr := a.restoreAuthPriority(context.Background(), *session); restoreErr != nil {
		note := restoreFailedMessage + "：" + restoreErr.Error()
		result.Detail = joinDetail(result.Detail, note)
		if result.Success {
			if result.Reply != "" {
				result.Reply = result.Reply + "（" + restoreFailedMessage + "）"
			} else {
				result.Reply = restoreFailedMessage
			}
		}
	}
	return result
}

func (a *Activator) prepareBoost(ctx context.Context, authID string) (*boostSession, error) {
	if a == nil || a.Host == nil || strings.TrimSpace(authID) == "" {
		return nil, nil
	}
	files, err := a.Host.ListAuthFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("列举凭证失败")
	}
	target, ok := findAuthFile(files, authID)
	if !ok {
		return nil, fmt.Errorf("未找到凭证")
	}
	name, data, err := a.loadPhysicalJSON(ctx, target)
	if err != nil || !looksLikeAuthDocument(data) {
		// 唯一最高时不必读完整文件；先按 List 判断，缺文件只在需要抬时失败。
		provider := providerOf(target)
		current := priorityOfAuthFile(target).value
		maxPriority, peerAtMax := providerMaxPriorityStats(files, provider)
		if maxPriority < current {
			maxPriority = current
		}
		if !needsPriorityBoost(current, maxPriority, peerAtMax) {
			return nil, nil
		}
		return nil, fmt.Errorf("无法读取凭证")
	}
	updated := append([]host.AuthFile(nil), files...)
	for i := range updated {
		if matchAuth(updated[i], authID) {
			updated[i].Data = data
		}
	}
	provider := providerOf(target)
	original := snapshotFromJSON(data)
	current := original.value
	maxPriority, peerAtMax := providerMaxPriorityStats(updated, provider)
	if !needsPriorityBoost(current, maxPriority, peerAtMax) {
		return nil, nil
	}
	boosted := boostedPriority(maxPriority)
	patched, err := applyPriority(data, prioritySnapshot{present: true, value: boosted})
	if err != nil {
		return nil, fmt.Errorf("提升凭证优先级失败")
	}
	if err := a.Host.SaveAuthFile(ctx, name, patched); err != nil {
		return nil, fmt.Errorf("提升凭证优先级失败")
	}
	if err := a.confirmPriority(ctx, target, name, prioritySnapshot{present: true, value: boosted}); err != nil {
		_ = a.writePriority(ctx, target, name, original)
		return nil, fmt.Errorf("优先级写入后未能确认")
	}
	return &boostSession{file: target, name: name, original: original}, nil
}

func (a *Activator) restoreAuthPriority(ctx context.Context, session boostSession) error {
	if err := a.writePriority(ctx, session.file, session.name, session.original); err != nil {
		return err
	}
	return a.confirmPriority(ctx, session.file, session.name, session.original)
}

func (a *Activator) writePriority(ctx context.Context, file host.AuthFile, name string, snap prioritySnapshot) error {
	_, data, err := a.loadPhysicalJSON(ctx, file)
	if err != nil || !looksLikeAuthDocument(data) {
		return fmt.Errorf("无法读取凭证")
	}
	patched, err := applyPriority(data, snap)
	if err != nil {
		return err
	}
	if err := a.Host.SaveAuthFile(ctx, name, patched); err != nil {
		return err
	}
	return nil
}

func (a *Activator) confirmPriority(ctx context.Context, file host.AuthFile, name string, want prioritySnapshot) error {
	gotFile := file
	gotFile.Name = firstNonBlank(name, file.Name)
	_, data, err := a.loadPhysicalJSON(ctx, gotFile)
	if err != nil || !looksLikeAuthDocument(data) {
		return fmt.Errorf("无法读取凭证")
	}
	got := snapshotFromJSON(data)
	if got.present != want.present || (want.present && got.value != want.value) {
		return fmt.Errorf("回写后优先级与原值不一致")
	}
	return nil
}

func needsPriorityBoost(current, maxPriority, peerAtMax int) bool {
	if current < maxPriority {
		return true
	}
	return current == maxPriority && peerAtMax >= 2
}

func boostedPriority(maxPriority int) int {
	next := maxPriority + 1
	if next < priorityBoostFloor {
		return priorityBoostFloor
	}
	return next
}

func providerOf(file host.AuthFile) string {
	return strings.ToLower(strings.TrimSpace(firstNonBlank(file.Provider, file.Type)))
}

func providerMaxPriorityStats(files []host.AuthFile, provider string) (maxPriority int, peerAtMax int) {
	hasPeer := false
	for _, file := range files {
		if providerOf(file) != provider {
			continue
		}
		value := priorityOfAuthFile(file).value
		if !hasPeer || value > maxPriority {
			maxPriority = value
			peerAtMax = 1
			hasPeer = true
			continue
		}
		if value == maxPriority {
			peerAtMax++
		}
	}
	return maxPriority, peerAtMax
}

func priorityOfAuthFile(file host.AuthFile) prioritySnapshot {
	if len(file.Data) > 0 {
		return snapshotFromJSON(file.Data)
	}
	if file.Attributes != nil {
		if raw, ok := file.Attributes["priority"]; ok {
			if value, ok := intFromAny(raw); ok {
				return prioritySnapshot{present: true, value: value}
			}
		}
	}
	return prioritySnapshot{}
}

func snapshotFromJSON(data []byte) prioritySnapshot {
	var document map[string]json.RawMessage
	if json.Unmarshal(data, &document) != nil {
		return prioritySnapshot{}
	}
	raw, ok := document["priority"]
	if !ok {
		return prioritySnapshot{}
	}
	value, ok := intFromRaw(raw)
	if !ok {
		return prioritySnapshot{}
	}
	return prioritySnapshot{present: true, value: value}
}

func intFromRaw(raw json.RawMessage) (int, bool) {
	var value int
	if json.Unmarshal(raw, &value) == nil {
		return value, true
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		parsed, err := strconv.Atoi(strings.TrimSpace(text))
		return parsed, err == nil
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil {
		return int(number), true
	}
	return 0, false
}

func intFromAny(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		parsed, err := value.Int64()
		return int(parsed), err == nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		return parsed, err == nil
	case json.RawMessage:
		return intFromRaw(value)
	default:
		return 0, false
	}
}

func applyPriority(data []byte, snap prioritySnapshot) ([]byte, error) {
	document := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &document); err != nil || len(document) == 0 {
		return nil, fmt.Errorf("凭证文档无效")
	}
	if !snap.present {
		delete(document, "priority")
		return json.Marshal(document)
	}
	value, err := json.Marshal(snap.value)
	if err != nil {
		return nil, err
	}
	document["priority"] = value
	return json.Marshal(document)
}

func (a *Activator) loadPhysicalJSON(ctx context.Context, file host.AuthFile) (string, []byte, error) {
	name := firstNonBlank(file.Name, file.ID, file.AuthIndex)
	for _, key := range uniqueNonBlank(file.AuthIndex, file.Name, file.ID, name) {
		physical, err := a.Host.GetAuthFile(ctx, key)
		if err != nil || len(physical.Data) == 0 {
			continue
		}
		gotName := firstNonBlank(physical.Name, name, key)
		if looksLikeAuthDocument(physical.Data) {
			return gotName, append([]byte(nil), physical.Data...), nil
		}
	}
	if looksLikeAuthDocument(file.Data) {
		return name, append([]byte(nil), file.Data...), nil
	}
	return name, nil, fmt.Errorf("无法读取凭证")
}

func findAuthFile(files []host.AuthFile, authID string) (host.AuthFile, bool) {
	for _, file := range files {
		if matchAuth(file, authID) {
			return file, true
		}
	}
	return host.AuthFile{}, false
}

func looksLikeAuthDocument(data []byte) bool {
	if len(data) < 64 {
		return false
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(data, &document) != nil {
		return false
	}
	for _, key := range []string{"access_token", "refresh_token", "id_token", "api_key"} {
		if raw, ok := document[key]; ok && len(strings.TrimSpace(string(raw))) > 2 && string(raw) != "null" {
			return true
		}
	}
	if raw, ok := document["tokens"]; ok {
		var tokens map[string]json.RawMessage
		if json.Unmarshal(raw, &tokens) == nil {
			for _, key := range []string{"access_token", "refresh_token", "id_token", "api_key"} {
				if item, ok := tokens[key]; ok && len(strings.TrimSpace(string(item))) > 2 && string(item) != "null" {
					return true
				}
			}
		}
	}
	return false
}

func uniqueNonBlank(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func joinDetail(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "；")
}
