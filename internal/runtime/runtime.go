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
	"quota-schedule-refresh/internal/plan"
	"quota-schedule-refresh/internal/store"
	"quota-schedule-refresh/internal/wake"
)

const pluginVersion = "0.7.16"

const busyRetryInterval = 30 * time.Second

// noModelMessage 是彻底找不到模型时记进执行记录的原因。猜一个模型名去试，
// 换来的只是一条「模型 xxx 不可用」，把真正的原因藏了起来。
const noModelMessage = "没有可用模型：CPA 未返回任何模型，凭证也没带模型列表，请在设置里指定模型"

type historyEntry struct {
	At      time.Time     `json:"at"`
	Trigger string        `json:"trigger"`
	Message string        `json:"message"`
	Results []wake.Result `json:"results"`
}

type Runtime struct {
	mu            sync.Mutex
	host          host.Client
	settings      *store.Store
	baseConfig    config.Config
	config        config.Config
	now           func() time.Time
	cancel        context.CancelFunc
	shutdown      bool
	lastScanAt    time.Time
	nextScanAt    time.Time
	lastRun       []wake.Result
	lastMessage   string
	history       []historyEntry
	preferredAuth string
	fallbackMu    sync.Mutex
	modelsMu      sync.Mutex
	modelsCache   []string
	// modelsSyncedAt 是缓存上次被成功填充的时刻，modelsTriedAt 是上次发起查询的时刻。
	// 两者分开记：查询失败时要保留旧缓存，但仍需退避，不能每个请求都重试。
	modelsSyncedAt time.Time
	modelsTriedAt  time.Time
	// modelsInflight 非 nil 表示后台刷新进行中，关闭即代表结束，用于合并并发查询。
	// modelsRefreshAt 是它的发起时刻：卡死的刷新永远不会清掉这个标记，
	// 所以只在 modelsRefreshDeadline 之内相信它。
	modelsInflight  chan struct{}
	modelsRefreshAt time.Time
	modelsNow       func() time.Time
	modelsQuery     func() []string
	// stopped 在 Shutdown 时关闭，让冷启动的同步等待立即返回，不必等宿主回调。
	stopped     chan struct{}
	running     bool
	scheduleKey string
}

func New(hostClient host.Client) *Runtime {
	return &Runtime{
		host:       hostClient,
		settings:   store.New(""),
		baseConfig: config.Default(),
		config:     config.Default(),
		now:        time.Now,
		stopped:    make(chan struct{}),
	}
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
	base, err := config.Parse([]byte(yamlText))
	if err != nil {
		return failure(err)
	}
	if err := r.replaceConfig(base, r.mergeStoredSettings(base)); err != nil {
		return failure(err)
	}
	// 预热缓存，让设置页第一次打开就有模型可选，不必靠某个请求去触发冷启动。
	go r.scheduleModelsRefresh()
	return envelopeResult(r.registrationResult(), nil)
}

// mergeStoredSettings 把插件页面保存过的设置叠加到宿主配置之上。
// 私有文件损坏或不可读时退回宿主配置，不让插件注册失败。
func (r *Runtime) mergeStoredSettings(base config.Config) config.Config {
	data, found, err := r.settings.Load()
	if err != nil || !found {
		return base
	}
	merged, err := config.ApplyOver(base, data)
	if err != nil {
		return base
	}
	return merged
}

func (r *Runtime) replaceConfig(base, cfg config.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shutdown {
		return ErrShutdown
	}
	r.baseConfig = base
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
	alreadyDown := r.shutdown
	r.shutdown = true
	r.mu.Unlock()
	// 宿主重复调用 shutdown 时不能重复 close，否则 panic。
	if !alreadyDown && r.stopped != nil {
		close(r.stopped)
	}
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
		if wait > 0 {
			if !sleepCtx(ctx, wait) {
				return
			}
		} else if r.skipMissedSchedule() {
			// 热更新或重启发生在今天的点之后：等到明天，不把过点的定时补跑一遍。
			continue
		}
		// 手动执行占用时定时触发会被跳过，此处退避避免空转。
		if !r.runOnce(ctx, "schedule", nil) && !sleepCtx(ctx, busyRetryInterval) {
			return
		}
	}
}

// skipMissedSchedule 只在 wait==0 时调用。等到点再醒的那次 wait 当时大于 0，不会进来。
func (r *Runtime) skipMissedSchedule() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	today := r.config.TodayTrigger(now)
	if now.Before(today) {
		return false
	}
	ranToday := !r.lastScanAt.IsZero() && !r.lastScanAt.In(r.config.Location()).Before(today)
	if ranToday {
		return false
	}
	r.lastScanAt = today
	r.nextScanAt = r.config.NextTrigger(now)
	return true
}

func (r *Runtime) preScanWait() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	wait := r.config.WaitUntilScan(now, r.lastScanAt)
	r.nextScanAt = now.Add(wait)
	return wait
}

func (r *Runtime) runOnce(ctx context.Context, trigger string, authIDs []string) bool {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return false
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
	return true
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
	fallbackModel := r.defaultModel()
	targets := make([]candidate, 0, len(files))
	skipped := make([]wake.Result, 0)
	wanted := selectedAuthSet(authIDs)
	for _, file := range files {
		enriched := r.enrichAuthFile(ctx, file)
		target, included := candidateFromFile(cfg, enriched, fallbackModel)
		if !included {
			continue
		}
		if len(wanted) > 0 && !wanted[target.authID] && !wanted[target.label] {
			continue
		}
		// 显式勾选凭证时按用户意图执行，跳过规则只作用于全量（定时）刷新。
		if cfg.SkipGPTPro && target.skipSchedule && len(wanted) == 0 {
			skipped = append(skipped, wake.Result{
				AuthID:  target.authID,
				Label:   target.label,
				Model:   target.model,
				Status:  "skipped",
				Success: false,
				Reply:   target.skipReply,
			})
			continue
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		if len(skipped) > 0 {
			return skipped, fmt.Sprintf("成功 0，失败 0，跳过 %d", len(skipped))
		}
		if len(wanted) > 0 {
			return nil, "没有匹配的 Codex 凭证"
		}
		return nil, "没有可唤醒的 Codex 凭证"
	}
	// 逐个执行：刷新前要临时提升凭证优先级并把它设为 CPA 的首选账号，
	// 这些都是全局状态，并行会互相覆盖（见 wake.withBoostedAuth）。
	collected := make([]wake.Result, 0, len(targets))
	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		// 没有模型就没有可发的请求。以前这里会拿一个硬编码的名字去试，
		// 失败信息变成「模型 gpt-5-mini 不可用」，掩盖了真正的原因。
		if target.model == "" {
			collected = append(collected, wake.Result{
				AuthID:    target.authID,
				Label:     target.label,
				Status:    "failed",
				Success:   false,
				LastError: noModelMessage,
			})
			continue
		}
		collected = append(collected, activator.Activate(ctx, target.authID, target.label, target.model, target.disabled))
	}
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
	return append(skipped, collected...), fmt.Sprintf("成功 %d，失败 %d，跳过 %d", ok, fail, skip+len(skipped))
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

// defaultModel 是配置留空时替用户选用的模型，同时也是 /status 里展示的
// default_model。一个都拿不到时返回空串，由调用方决定怎么说明。
func (r *Runtime) defaultModel() string {
	return preferredFallbackModel(r.availableModels())
}

type candidate struct {
	authID       string
	label        string
	model        string
	disabled     bool
	skipSchedule bool
	skipReply    string
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
	planType := plan.FromAuth([]string{file.Name, file.ID, file.AuthIndex}, file.Data, file.Metadata, file.Attributes)
	return candidate{
		authID:       authID,
		label:        label,
		model:        model,
		disabled:     file.Disabled,
		skipSchedule: plan.SkipOnSchedule(planType),
		skipReply:    plan.SkipReason(planType),
	}, true
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
	if picked := preferredFallbackModel(available); picked != "" {
		return picked
	}
	return strings.TrimSpace(fallback)
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

// Metadata 故意不声明 ConfigFields。宿主的 config_fields 协议只有
// name/type/enum_values/description 四项，无处表达默认值，导致自动生成的表单
// 把留空项渲染成空值、布尔项一律渲染成关闭，与插件实际生效的默认值不一致。
// 设置改由插件自己的页面维护，见 handleSettings。
type Metadata struct {
	Name             string `json:"Name"`
	Version          string `json:"Version"`
	Author           string `json:"Author"`
	GitHubRepository string `json:"GitHubRepository"`
	Description      string `json:"Description"`
}

type RegisterResult struct {
	SchemaVersion int             `json:"schema_version"`
	Metadata      Metadata        `json:"metadata"`
	Capabilities  map[string]bool `json:"capabilities"`
}

func (r *Runtime) registrationResult() RegisterResult {
	return RegisterResult{
		SchemaVersion: 1,
		Metadata: Metadata{
			Name:             "Quota Schedule Refresh",
			Version:          pluginVersion,
			Author:           "glj",
			GitHubRepository: "https://github.com/glj2369/quota-schedule-refresh",
			Description:      "每天按设定时刻通过 CPA 接口刷新 Codex 额度窗口。设置在插件页面内维护。",
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
	ScheduleEnabled bool           `json:"schedule_enabled"`
	DailyAt         string         `json:"daily_at"`
	Timezone        string         `json:"timezone"`
	Model           string         `json:"model"`
	DefaultModel    string         `json:"default_model"`
	RetryCount      int            `json:"retry_count"`
	RetryInterval   int            `json:"retry_interval_seconds"`
	SkipGPTPro      bool           `json:"skip_gpt_pro"`
	NextScanAt      time.Time      `json:"next_scan_at"`
	LastScanAt      time.Time      `json:"last_scan_at"`
	LastMessage     string         `json:"last_message"`
	LastRun         []wake.Result  `json:"last_run"`
	History         []historyEntry `json:"history"`
	Version         string         `json:"version"`
}

func (r *Runtime) currentStatus() statusPayload {
	listed := r.defaultModel()
	r.mu.Lock()
	defer r.mu.Unlock()
	return statusPayload{
		ScheduleEnabled: r.config.ScheduleEnabled,
		DailyAt:         r.config.DailyAt,
		Timezone:        r.config.Timezone,
		Model:           r.config.Model,
		DefaultModel:    listed,
		RetryCount:      r.config.RetryCount,
		RetryInterval:   int(r.config.RetryInterval / time.Second),
		SkipGPTPro:      r.config.SkipGPTPro,
		NextScanAt:      r.nextScanAt,
		LastScanAt:      r.lastScanAt,
		LastMessage:     r.lastMessage,
		LastRun:         append([]wake.Result(nil), r.lastRun...),
		History:         append([]historyEntry(nil), r.history...),
		Version:         pluginVersion,
	}
}

type credentialPayload struct {
	AuthID       string `json:"auth_id"`
	Label        string `json:"label"`
	Plan         string `json:"plan,omitempty"`
	GPTPro       bool   `json:"gpt_pro,omitempty"`
	SkipSchedule bool   `json:"skip_schedule,omitempty"`
	Disabled     bool   `json:"disabled"`
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
		planType := plan.FromAuth([]string{enriched.Name, enriched.ID, enriched.AuthIndex}, enriched.Data, enriched.Metadata, enriched.Attributes)
		out = append(out, credentialPayload{
			AuthID:       authID,
			Label:        label,
			Plan:         planType,
			GPTPro:       plan.IsGPTPro(planType),
			SkipSchedule: plan.SkipOnSchedule(planType),
			Disabled:     enriched.Disabled,
		})
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
