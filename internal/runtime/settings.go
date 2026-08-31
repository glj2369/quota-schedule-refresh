package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"

	"quota-schedule-refresh/internal/config"
)

type settingsPayload struct {
	Settings config.Settings `json:"settings"`
	Models   []string        `json:"models"`
	Path     string          `json:"path"`
	Stored   bool            `json:"stored"`
}

func (r *Runtime) settingsView() settingsPayload {
	models := r.availableModels()
	_, stored, _ := r.settings.Load()
	r.mu.Lock()
	current := r.config
	r.mu.Unlock()
	return settingsPayload{
		Settings: config.ToSettings(current),
		Models:   models,
		Path:     r.settings.Path(),
		Stored:   stored,
	}
}

// saveSettings 先校验再落盘，最后热应用，避免写入无法启动的配置。
func (r *Runtime) saveSettings(body []byte) (settingsPayload, error) {
	var wire struct {
		Settings *config.Settings `json:"settings"`
	}
	incoming := config.Settings{}
	if err := json.Unmarshal(body, &wire); err == nil && wire.Settings != nil {
		incoming = *wire.Settings
	} else if err := json.Unmarshal(body, &incoming); err != nil {
		return settingsPayload{}, fmt.Errorf("%w: 设置内容无法解析", ErrInvalidRequest)
	}

	r.mu.Lock()
	base := r.baseConfig
	r.mu.Unlock()

	merged, err := incoming.Apply(base)
	if err != nil {
		return settingsPayload{}, err
	}
	encoded, err := config.ToSettings(merged).Encode()
	if err != nil {
		return settingsPayload{}, err
	}
	if err := r.settings.Save(encoded); err != nil {
		return settingsPayload{}, fmt.Errorf("写入设置失败：%v", err)
	}
	if err := r.replaceConfig(base, merged); err != nil {
		return settingsPayload{}, err
	}
	return r.settingsView(), nil
}

func (r *Runtime) handleSettings(w http.ResponseWriter, request *http.Request, body []byte) {
	if request.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, r.settingsView())
		return
	}
	view, err := r.saveSettings(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, view)
}
