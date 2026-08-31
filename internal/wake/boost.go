package wake

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"quota-schedule-refresh/internal/host"
)

const priorityBoostFloor = 1000

func (a *Activator) withBoostedAuth(ctx context.Context, authID string, fn func(context.Context) Result) Result {
	a.executeMu.Lock()
	defer a.executeMu.Unlock()

	restore := func(context.Context) {}
	if boosted, ok := a.boostAuthPriority(ctx, authID); ok {
		restore = boosted
	}
	defer restore(context.Background())
	return fn(ctx)
}

func (a *Activator) boostAuthPriority(ctx context.Context, authID string) (func(context.Context), bool) {
	noop := func(context.Context) {}
	if a == nil || a.Host == nil || strings.TrimSpace(authID) == "" {
		return noop, false
	}
	files, err := a.Host.ListAuthFiles(ctx)
	if err != nil {
		return noop, false
	}
	target, ok := findAuthFile(files, authID)
	if !ok {
		return noop, false
	}
	name, data, err := a.loadPhysicalJSON(ctx, target)
	if err != nil || !looksLikeAuthDocument(data) {
		return noop, false
	}
	patched, err := withPriority(data, priorityBoostFloor)
	if err != nil {
		return noop, false
	}
	if err := a.Host.SaveAuthFile(ctx, name, patched); err != nil {
		return noop, false
	}
	original := append([]byte(nil), data...)
	restored := false
	return func(restoreCtx context.Context) {
		if restored {
			return
		}
		restored = true
		if restoreCtx == nil {
			restoreCtx = context.Background()
		}
		_ = a.Host.SaveAuthFile(restoreCtx, name, original)
	}, true
}

func (a *Activator) loadPhysicalJSON(ctx context.Context, file host.AuthFile) (string, []byte, error) {
	name := firstNonBlank(file.Name, file.ID, file.AuthIndex)
	if looksLikeAuthDocument(file.Data) {
		return name, append([]byte(nil), file.Data...), nil
	}
	for _, key := range []string{file.AuthIndex, file.Name, file.ID} {
		if strings.TrimSpace(key) == "" {
			continue
		}
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
		return name, file.Data, nil
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

func withPriority(data []byte, priority int) ([]byte, error) {
	document := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &document); err != nil || len(document) == 0 {
		return nil, fmt.Errorf("凭证文档无效")
	}
	value, err := json.Marshal(priority)
	if err != nil {
		return nil, err
	}
	document["priority"] = value
	return json.Marshal(document)
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

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
