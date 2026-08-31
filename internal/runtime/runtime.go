package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"quota-schedule-refresh/internal/config"
	"quota-schedule-refresh/internal/host"
	"quota-schedule-refresh/internal/wake"
)

const pluginVersion = "0.4.0"

type historyEntry struct {
	At      time.Time     `json:"at"`
	Trigger string        `json:"trigger"`
	Message string        `json:"message"`
	Results []wake.Result `json:"results"`
}

type Runtime struct {
	mu             sync.Mutex
	host           host.Client
	config         config.Config
	now            func() time.Time
	cancel         context.CancelFunc
	shutdown       bool
	lastScanAt     time.Time
	nextScanAt     time.Time
	lastRun        []wake.Result
	lastMessage    string
	history        []historyEntry
	preferredAuth  string
	fallbackMu     sync.Mutex
	running        bool
	scheduleKey    string
}

func New(hostClient host.Client) *Runtime {
	return &Runtime{host: hostClient, config: config.Default(), now: time.Now}
}

func (r *Runtime) Handle(ctx context.Context, method string, request []byte) []byte {
	switch method {
	case "plugin.register":
		return r.register(request)
	case "plugin.reconfigure":
		return r.register(request)
	case "plugin.shutdown":
		return envelopeStatus(r.Shutdown())
	case "management.register":
		return r.registerManagement()
	case "management.handle":
		return r.handleManagement(ctx, request)
	case "scheduler.pick":
		return r.pickSchedule(request)
	default:
		return failure(fmt.Errorf("%w: method %q", ErrInvalidRequest, method))
	}
}

func (r *Runtime) register(raw []byte) []byte {
	yamlText, err := decodeConfigYAML(raw)
	if err != nil {
		return failure(err)
	}
	cfg, err := config.Parse([]byte(yamlText))
	if err != nil {
		return failure(err)
	}
	if err := r.replaceConfig(cfg); err != nil {
		return failure(err)
	}
	return envelopeResult(r.registrationResult(), nil)
}

func (r *Runtime) replaceConfig(cfg config.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shutdown {
		return ErrShutdown
	}
	r.config = cfg
	key := fmt.Sprintf("%t|%s|%s", cfg.ScheduleEnabled, cfg.DailyAt, cfg.Timezone)
	if !cfg.ScheduleEnabled {
		r.stopLocked()
		r.scheduleKey = ""
		return nil
	}
	if r.cancel != nil && r.scheduleKey == key {
		return nil
	}
	r.stopLocked()
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.scheduleKey = key
	go r.runLoop(ctx)
	return nil
}

func (r *Runtime) stopLocked() {
	if r.cancel == nil {
		return
	}
	r.cancel()
	r.cancel = nil
}

func (r *Runtime) Shutdown() error {
	r.mu.Lock()
	r.stopLocked()
	r.shutdown = true
	r.mu.Unlock()
	return nil
}

func (r *Runtime) runLoop(ctx context.Context) {
	if !sleepCtx(ctx, 3*time.Second) {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		wait := r.preScanWait()
		if wait > 0 && !sleepCtx(ctx, wait) {
			return
		}
		r.runOnce(ctx, "schedule", nil)
	}
}

func (r *Runtime) preScanWait() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	wait := r.config.WaitUntilScan(now, r.lastScanAt)
	r.nextScanAt = now.Add(wait)
	return wait
}

func (r *Runtime) runOnce(ctx context.Context, trigger string, authIDs []string) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	cfg := r.config
	now := r.now()
	if trigger == "schedule" {
		r.lastScanAt = now
		r.nextScanAt = cfg.NextTrigger(now)
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	results, message := r.activateAll(ctx, cfg, authIDs)
	entry := historyEntry{
		At:      now,
		Trigger: trigger,
		Message: message,
		Results: append([]wake.Result(nil), results...),
	}
	r.mu.Lock()
	r.lastRun = results
	r.lastMessage = trigger + "：" + message
	r.history = append([]historyEntry{entry}, r.history...)
	if len(r.history) > 5 {
		r.history = r.history[:5]
	}
	r.mu.Unlock()
}

func (r *Runtime) activateAll(ctx context.Context, cfg config.Config, authIDs []string) ([]wake.Result, string) {
	if r.host == nil {
		return nil, "缺少宿主依赖"
	}
	files, err := r.host.ListAuthFiles(ctx)
	if err != nil {
		return nil, "列举凭证失败"
	}
	activator := &wake.Activator{
		Host:             r.host,
		Config:           cfg,
		PinPreferredAuth: r.pinPreferredAuth,
	}
	fallbackModel := r.firstListedModel()
	targets := make([]candidate, 0, len(files))
	for _, file := range files {
		enriched := r.enrichAuthFile(ctx, file)
		target, included := candidateFromFile(cfg, enriched, fallbackModel)
		if included {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return nil, "没有可唤醒的 Codex 凭证"
	}
	if wanted := selectedAuthSet(authIDs); len(wanted) > 0 {
		filtered := make([]candidate, 0, len(wanted))
		for _, target := range targets {
			if wanted[target.authID] || wanted[target.label] {
				filtered = append(filtered, target)
			}
		}
		targets = filtered
	}
	if len(targets) == 0 {
		return nil, "没有匹配的 Codex 凭证"
	}
	workers := cfg.MaxConcurrency
	if workers < 1 {
		workers = 1
	}
	if workers > len(targets) {
		workers = len(targets)
	}
	jobs := make(chan candidate, len(targets))
	collected := make([]wake.Result, 0, len(targets))
	var collectedMu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range jobs {
				if ctx.Err() != nil {
					continue
				}
				item := activator.Activate(ctx, target.authID, target.label, target.model, target.disabled)
				collectedMu.Lock()
				collected = append(collected, item)
				collectedMu.Unlock()
			}
		}()
	}
	for _, target := range targets {
		jobs <- target
	}
	close(jobs)
	wg.Wait()
	ok, fail, skip := 0, 0, 0
	for _, item := range collected {
		switch {
		case item.Success:
			ok++
		case item.Status == "skipped":
			skip++
		default:
			fail++
		}
	}
	return collected, fmt.Sprintf("成功 %d，失败 %d，跳过 %d", ok, fail, skip)
}

func (r *Runtime) enrichAuthFile(ctx context.Context, file host.AuthFile) host.AuthFile {
	if r.host == nil {
		return file
	}
	key := firstNonBlank(file.AuthIndex, file.ID, file.Name)
	if key == "" {
		return file
	}
	runtimeFile, err := r.host.GetRuntimeAuthFile(ctx, key)
	if err != nil {
		return file
	}
	if len(runtimeFile.Models) > 0 {
		file.Models = runtimeFile.Models
	}
	if len(runtimeFile.RecentModels) > 0 {
		file.RecentModels = runtimeFile.RecentModels
	}
	if len(runtimeFile.Data) > 0 && len(file.Data) == 0 {
		file.Data = runtimeFile.Data
	}
	if file.Provider == "" {
		file.Provider = runtimeFile.Provider
	}
	if file.Type == "" {
		file.Type = runtimeFile.Type
	}
	if len(runtimeFile.Metadata) > 0 {
		file.Metadata = runtimeFile.Metadata
	}
	if len(runtimeFile.Attributes) > 0 {
		file.Attributes = runtimeFile.Attributes
	}
	if len(runtimeFile.ModelQuotas) > 0 {
		file.ModelQuotas = runtimeFile.ModelQuotas
	}
	return file
}

func (r *Runtime) pinPreferredAuth(authID string) func() {
	r.fallbackMu.Lock()
	r.mu.Lock()
	r.preferredAuth = authID
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.preferredAuth = ""
		r.mu.Unlock()
		r.fallbackMu.Unlock()
	}
}

func (r *Runtime) listedModels() []string {
	if r == nil || r.host == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	files, err := r.host.ListAuthFiles(ctx)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, file := range files {
		enriched := r.enrichAuthFile(ctx, file)
		if !isCodex(enriched) {
			continue
		}
		for _, model := range collectModels(enriched) {
			if seen[model] {
				continue
			}
			seen[model] = true
			out = append(out, model)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *Runtime) firstListedModel() string {
	models := r.cpaModels()
	if len(models) == 0 {
		models = r.listedModels()
	}
	if len(models) > 0 {
		return models[0]
	}
	return "gpt-5-mini"
}

type candidate struct {
	authID   string
	label    string
	model    string
	disabled bool
}

func candidateFromFile(cfg config.Config, file host.AuthFile, fallbackModel string) (candidate, bool) {
	if !isCodex(file) {
		return candidate{}, false
	}
	authID := firstNonBlank(file.ID, file.Name, file.AuthIndex)
	if authID == "" {
		return candidate{}, false
	}
	if file.Disabled && !cfg.EnableDisabled {
		return candidate{}, false
	}
	model := chooseModel(cfg.Model, collectModels(file), fallbackModel)
	label := firstNonBlank(file.Account, file.Email, file.Name, authID)
	return candidate{authID: authID, label: label, model: model, disabled: file.Disabled}, true
}

func isCodex(file host.AuthFile) bool {
	text := strings.ToLower(firstNonBlank(file.Provider, file.Type, file.Name, file.ID))
	return strings.Contains(text, "codex")
}

func collectModels(file host.AuthFile) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	add := func(values []string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	add(file.Models)
	add(file.RecentModels)
	add(stringSliceFromMap(file.Metadata, "available_models", "availableModels", "models"))
	add(stringSliceFromMap(file.Attributes, "available_models", "availableModels", "models"))
	add(objectKeysInOrder(file.ModelQuotas))
	var document struct {
		AvailableModels []string `json:"available_models"`
		Models          []string `json:"models"`
		RecentModels    []string `json:"recent_models"`
	}
	if json.Unmarshal(file.Data, &document) == nil {
		add(document.AvailableModels)
		add(document.Models)
		add(document.RecentModels)
	}
	return out
}

func stringSliceFromMap(values map[string]any, keys ...string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0)
	for _, key := range keys {
		switch raw := values[key].(type) {
		case []string:
			out = append(out, raw...)
		case []any:
			for _, item := range raw {
				if value, ok := item.(string); ok {
					out = append(out, value)
				}
			}
		case string:
			for _, item := range strings.Split(raw, ",") {
				out = append(out, item)
			}
		}
	}
	return out
}

func objectKeysInOrder(raw json.RawMessage) []string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	keys := make([]string, 0)
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			break
		}
		key, _ := keyToken.(string)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			break
		}
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func chooseModel(configured string, available []string, fallback string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured
	}
	if len(available) > 0 {
		return available[0]
	}
	if trimmed := strings.TrimSpace(fallback); trimmed != "" {
		return trimmed
	}
	return "gpt-5-mini"
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

type ConfigField struct {
	Name         string   `json:"Name"`
	Type         string   `json:"Type"`
	Description  string   `json:"Description"`
	EnumValues   []string `json:"EnumValues,omitempty"`
	DefaultValue any      `json:"DefaultValue"`
}

type Metadata struct {
	Name         string        `json:"Name"`
	Version      string        `json:"Version"`
	Author       string        `json:"Author"`
	GitHubRepository string        `json:"GitHubRepository"`
	Description      string        `json:"Description"`
	ConfigFields []ConfigField `json:"ConfigFields,omitempty"`
}

type RegisterResult struct {
	SchemaVersion int               `json:"schema_version"`
	Metadata      Metadata          `json:"metadata"`
	Capabilities  map[string]bool   `json:"capabilities"`
}

func (r *Runtime) registrationResult() RegisterResult {
	defaults := config.Default()
	models := r.cpaModels()
	if len(models) == 0 {
		models = r.listedModels()
	}
	listed := ""
	if len(models) > 0 {
		listed = models[0]
	}
	modelType := "string"
	if len(models) > 0 {
		modelType = "enum"
	}
	return RegisterResult{
		SchemaVersion: 1,
		Metadata: Metadata{
			Name:             "Quota Schedule Refresh",
			Version:          pluginVersion,
			Author:           "ssgs",
			GitHubRepository: "https://github.com/glj2369/quota-schedule-refresh",
			Description:      "Refresh Codex quota windows once a day at a configured local time.",
			ConfigFields: []ConfigField{
				{Name: "schedule_enabled", Type: "boolean", Description: "启用每日定时刷新额度窗口。默认 false。\nEnable the daily timer.", DefaultValue: defaults.ScheduleEnabled},
				{Name: "daily_at", Type: "string", Description: "每天触发时刻，格式 HH:MM，例如 08:00。\nDaily trigger time HH:MM.", DefaultValue: defaults.DailyAt},
				{Name: "timezone", Type: "string", Description: "时区，默认 Asia/Shanghai。\nIANA timezone.", DefaultValue: defaults.Timezone},
				{Name: "model", Type: modelType, Description: "从 CPA /v1/models 读取的 Codex 模型列表。默认第一项。刷新只走 CPA host.model.execute。\nCodex model from CPA /v1/models. Refresh uses CPA only.", EnumValues: models, DefaultValue: listed},
				{Name: "timeout_seconds", Type: "string", Description: "单次请求超时（秒）。默认 60。\nPer-request timeout in seconds.", DefaultValue: "60"},
				{Name: "enable_disabled", Type: "boolean", Description: "刷新前自动启用已禁用凭证。默认 true。\nRe-enable disabled credentials before refresh.", DefaultValue: defaults.EnableDisabled},
				{Name: "max_concurrency", Type: "integer", Description: "同时刷新的账号数上限（worker 池）。\nWorker pool size.", DefaultValue: defaults.MaxConcurrency},
				{Name: "prompt", Type: "string", Description: "刷新提示词。\nRefresh prompt.", DefaultValue: defaults.Prompt},
			},
		},
		Capabilities: map[string]bool{"management_api": true, "scheduler": true},
	}
}

func (r *Runtime) pickSchedule(raw []byte) []byte {
	type response struct {
		Handled bool   `json:"Handled"`
		AuthID  string `json:"AuthID,omitempty"`
		Reason  string `json:"Reason"`
	}
	r.mu.Lock()
	preferred := strings.TrimSpace(r.preferredAuth)
	r.mu.Unlock()
	if preferred == "" {
		return envelopeResult(response{Handled: false, Reason: "delegate"}, nil)
	}
	var wire struct {
		Candidates []struct {
			ID     string `json:"ID"`
			AuthID string `json:"AuthID"`
		} `json:"Candidates"`
	}
	_ = json.Unmarshal(raw, &wire)
	for _, candidate := range wire.Candidates {
		id := firstNonBlank(candidate.ID, candidate.AuthID)
		if id == preferred {
			return envelopeResult(response{Handled: true, AuthID: preferred, Reason: "preferred_auth"}, nil)
		}
	}
	return envelopeResult(response{Handled: false, Reason: "preferred_missing"}, nil)
}

type statusPayload struct {
	ScheduleEnabled bool          `json:"schedule_enabled"`
	DailyAt         string        `json:"daily_at"`
	Timezone        string        `json:"timezone"`
	Model           string        `json:"model"`
	DefaultModel    string        `json:"default_model"`
	MaxConcurrency  int           `json:"max_concurrency"`
	NextScanAt      time.Time     `json:"next_scan_at"`
	LastScanAt      time.Time     `json:"last_scan_at"`
	LastMessage     string         `json:"last_message"`
	LastRun         []wake.Result  `json:"last_run"`
	History         []historyEntry `json:"history"`
	Version         string         `json:"version"`
}

func (r *Runtime) currentStatus() statusPayload {
	listed := r.firstListedModel()
	r.mu.Lock()
	defer r.mu.Unlock()
	return statusPayload{
		ScheduleEnabled: r.config.ScheduleEnabled,
		DailyAt:         r.config.DailyAt,
		Timezone:        r.config.Timezone,
		Model:           r.config.Model,
		DefaultModel:    listed,
		MaxConcurrency:  r.config.MaxConcurrency,
		NextScanAt:      r.nextScanAt,
		LastScanAt:      r.lastScanAt,
		LastMessage:     r.lastMessage,
		LastRun:         append([]wake.Result(nil), r.lastRun...),
		History:         append([]historyEntry(nil), r.history...),
		Version:         pluginVersion,
	}
}

type credentialPayload struct {
	AuthID   string `json:"auth_id"`
	Label    string `json:"label"`
	Disabled bool   `json:"disabled"`
}

func (r *Runtime) listCredentials(ctx context.Context) []credentialPayload {
	if r.host == nil {
		return nil
	}
	files, err := r.host.ListAuthFiles(ctx)
	if err != nil {
		return nil
	}
	out := make([]credentialPayload, 0)
	for _, file := range files {
		enriched := r.enrichAuthFile(ctx, file)
		if !isCodex(enriched) {
			continue
		}
		authID := firstNonBlank(enriched.ID, enriched.Name, enriched.AuthIndex)
		if authID == "" {
			continue
		}
		label := firstNonBlank(enriched.Account, enriched.Email, enriched.Name, authID)
		out = append(out, credentialPayload{AuthID: authID, Label: label, Disabled: enriched.Disabled})
	}
	return out
}

func selectedAuthSet(authIDs []string) map[string]bool {
	wanted := map[string]bool{}
	for _, id := range authIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		wanted[id] = true
	}
	return wanted
}
