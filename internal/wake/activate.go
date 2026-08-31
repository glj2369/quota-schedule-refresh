package wake

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	UsedPath   string `json:"used_path,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

type Activator struct {
	Host             host.Client
	Config           config.Config
	PinPreferredAuth func(authID string) func()
}

func (a *Activator) Activate(ctx context.Context, authID, label, model string, disabled bool) Result {
	result := Result{AuthID: authID, Label: label, Model: model, Status: "failed", UsedPath: "cpa"}
	if a == nil || a.Host == nil {
		result.LastError = "CPA接口刷新失败：缺少宿主依赖"
		return result
	}
	callCtx := ctx
	cancel := func() {}
	if a.Config.Timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, a.Config.Timeout)
	}
	defer cancel()

	if disabled && a.Config.EnableDisabled {
		if err := a.enableAuth(callCtx, authID); err != nil {
			result.LastError = "启用已禁用凭证失败"
			return result
		}
	}

	return a.wakeCPA(callCtx, authID, model, result)
}

func (a *Activator) wakeCPA(ctx context.Context, authID, model string, previous Result) Result {
	previous.UsedPath = "cpa"
	if a.PinPreferredAuth != nil {
		if unpin := a.PinPreferredAuth(authID); unpin != nil {
			defer unpin()
		}
	}
	body, _ := json.Marshal(map[string]any{
		"model":  model,
		"stream": false,
		"messages": []map[string]string{{
			"role":    "user",
			"content": a.Config.Prompt,
		}},
	})
	response, err := a.Host.ModelExecute(ctx, host.ModelExecuteRequest{
		Model:   model,
		Stream:  false,
		Body:    body,
		Headers: map[string][]string{"X-Quota-Schedule-Refresh-Auth": {authID}},
	})
	if err != nil {
		previous.Status = "failed"
		previous.LastError = "CPA接口刷新失败：宿主模型执行失败"
		return previous
	}
	previous.HTTPStatus = response.StatusCode
	if !host.IsHTTPSuccess(response.StatusCode) {
		previous.Status = "failed"
		previous.LastError = fmt.Sprintf("CPA接口刷新失败：上游返回非成功状态（HTTP %d）", response.StatusCode)
		return previous
	}
	previous.Status = "success"
	previous.Success = true
	previous.LastError = ""
	return previous
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
