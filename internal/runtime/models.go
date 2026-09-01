package runtime

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"quota-schedule-refresh/internal/host"
)

// modelsCacheTTL 之内的缓存直接使用，不发任何请求。
const modelsCacheTTL = 5 * time.Minute

// modelsRetryInterval 是查询失败后的退避间隔。没有它，缓存一直为空时
// 每个请求都会重新发起一次查询。
const modelsRetryInterval = 30 * time.Second

// modelsColdStartWait 是进程内第一次查询允许的同步等待上限。
// 只有第一个请求可能等这么久，之后一律直接返回缓存。
const modelsColdStartWait = 3 * time.Second

// modelsQueryTimeout 是单次模型查询的超时。
const modelsQueryTimeout = 2 * time.Second

// modelsRefreshDeadline 是「已经有刷新在跑」这个判断的有效期。
//
// 后台刷新有可能永远回不来：凭证元数据那条路要走宿主回调，而 callHost 是同步
// cgo 调用，进入宿主之后连 defer 都不会再执行，modelsInflight 于是永远留着。
// 没有这个上限，一次卡死就永久停掉了后续每一次刷新——缓存为空时下拉列表再也
// 填不上，哪怕 CPA 早就恢复了。超过这个时限就允许新的刷新接替它。
const modelsRefreshDeadline = 10 * time.Second

// availableModels 缓存优先：命中未过期的缓存就直接返回，不发请求。
// 缓存过期只触发一次后台刷新并立即返回旧值；只有进程内从未查询过时才短暂同步等待。
//
// 请求路径上绝不能同步等宿主回调：callHost 是同步 cgo 调用，进入宿主后
// ctx 到期也打断不了，实测能把 /status 拖到 90 秒以上。
func (r *Runtime) availableModels() []string {
	if r == nil {
		return nil
	}
	cached, wait := r.modelsForRequest()
	if wait == nil {
		return sortedModels(cached)
	}
	timer := time.NewTimer(modelsColdStartWait)
	defer timer.Stop()
	select {
	case <-wait:
	case <-timer.C:
	case <-r.stopChannel():
	}
	cached, _ = r.modelsForRequest()
	return sortedModels(cached)
}

// scheduleModelsRefresh 只负责按缓存状态发起一次后台刷新，不等它完成。
func (r *Runtime) scheduleModelsRefresh() {
	if r == nil {
		return
	}
	r.modelsForRequest()
}

// modelsForRequest 返回当前缓存，并在需要时启动一次后台刷新。
// 第二个返回值非 nil 表示这是进程内第一次查询，调用方可以短暂等待它完成。
func (r *Runtime) modelsForRequest() ([]string, <-chan struct{}) {
	r.modelsMu.Lock()
	defer r.modelsMu.Unlock()
	cached := append([]string(nil), r.modelsCache...)
	now := r.modelsClockLocked()
	if len(cached) > 0 && now.Sub(r.modelsSyncedAt) < modelsCacheTTL {
		return cached, nil
	}
	if !r.modelsTriedAt.IsZero() && now.Sub(r.modelsTriedAt) < modelsRetryInterval {
		return cached, nil
	}
	coldStart := r.modelsTriedAt.IsZero()
	done := r.startModelsRefreshLocked(now)
	if coldStart {
		return cached, done
	}
	return cached, nil
}

// startModelsRefreshLocked 启动后台刷新。已有刷新在跑且还没超过 modelsRefreshDeadline
// 时复用它，超过了就另起一个接替。调用方必须持有 modelsMu。
func (r *Runtime) startModelsRefreshLocked(now time.Time) <-chan struct{} {
	if r.modelsInflight != nil && now.Sub(r.modelsRefreshAt) < modelsRefreshDeadline {
		return r.modelsInflight
	}
	if r.isStopped() {
		return nil
	}
	done := make(chan struct{})
	r.modelsInflight = done
	r.modelsRefreshAt = now
	r.modelsTriedAt = now
	go func() {
		found := r.runModelsQuery()
		r.modelsMu.Lock()
		// 查询失败（found 为空）时保留旧缓存：下拉列表宁可旧，不可空。
		if len(found) > 0 {
			r.modelsCache = append([]string(nil), found...)
			r.modelsSyncedAt = r.modelsClockLocked()
		}
		// 被接替的那次刷新可能很久以后才回来，它不能清掉接替者的在途标记。
		if r.modelsInflight == done {
			r.modelsInflight = nil
		}
		r.modelsMu.Unlock()
		close(done)
	}()
	return done
}

// runModelsQuery 是 queryModels 的注入点，只有测试会替换它。
func (r *Runtime) runModelsQuery() []string {
	if r.modelsQuery != nil {
		return r.modelsQuery()
	}
	return r.queryModels()
}

// queryModels 只允许在后台 goroutine 里调用。
//
// 直连既是首选也几乎是唯一的来源：它的 net/http 超时由插件自己的 goroutine 执行，
// 是这条路上唯一真正打得断的等待。CPA 应答了就以它的答案为准，即便答案是空的——
// 它此刻列不出的模型，拿去唤醒也只会换回一个 unknown provider。只有直连压根用不上
// （配置里读不到 api-key，或连不上 CPA）时才回落到凭证元数据，因为那条路要走宿主
// 回调，一旦卡住就再也回不来。
func (r *Runtime) queryModels() []string {
	models, answered := r.directModels()
	if len(models) > 0 {
		return models
	}
	if answered {
		return nil
	}
	return r.listedModels()
}

func (r *Runtime) modelsClockLocked() time.Time {
	if r.modelsNow != nil {
		return r.modelsNow()
	}
	return time.Now()
}

// stopChannel 用 nil channel 表示「永不触发」，select 会直接忽略该分支。
func (r *Runtime) stopChannel() <-chan struct{} {
	if r == nil {
		return nil
	}
	return r.stopped
}

func (r *Runtime) isStopped() bool {
	if r == nil || r.stopped == nil {
		return false
	}
	select {
	case <-r.stopped:
		return true
	default:
		return false
	}
}

// sortedModels 固定下拉列表的顺序：CPA 的 /v1/models 每次返回的次序都不同。
// gpt 系列排在前面，同系列内按版本号数值升序，这是下拉列表要的展示顺序。
// 注意升序意味着第一个是版本最低的，替用户选模型请走 preferredFallbackModel。
func sortedModels(models []string) []string {
	out := append([]string(nil), models...)
	sort.SliceStable(out, func(i, j int) bool {
		left, right := strings.ToLower(out[i]), strings.ToLower(out[j])
		if gi, gj := modelGroup(left), modelGroup(right); gi != gj {
			return gi < gj
		}
		return naturalLess(left, right)
	})
	return out
}

// preferredFallbackModel 挑用户没有配置模型时替他做主的那一个：候选里版本最高的
// gpt 系列模型。下拉列表按版本升序展示，直接取第一个会静默降级到最老的版本。
// 同一版本内取排序靠前的，于是 gpt-5.4 胜过 gpt-5.4-mini。
// 没有任何 gpt 系列模型时退回排序后的第一个，一个都没有时返回空串。
func preferredFallbackModel(models []string) string {
	ordered := sortedModels(models)
	best, bestVersion := "", ""
	for _, model := range ordered {
		version := gptVersion(strings.ToLower(model))
		if version == "" {
			continue
		}
		if best == "" || naturalLess(bestVersion, version) {
			best, bestVersion = model, version
		}
	}
	if best != "" {
		return best
	}
	if len(ordered) > 0 {
		return ordered[0]
	}
	return ""
}

// gptVersion 取 "gpt-" 后面的版本号段，比如 gpt-5.6-sol 得到 "5.6"。
// 不是 gpt 系列、或版本位上不是数字（gpt-image-2）时返回空串。
func gptVersion(lower string) string {
	if !strings.HasPrefix(lower, "gpt-") {
		return ""
	}
	rest := lower[len("gpt-"):]
	end := 0
	for end < len(rest) && (isDigit(rest[end]) || rest[end] == '.') {
		end++
	}
	return strings.Trim(rest[:end], ".")
}

func modelGroup(lower string) int {
	if strings.HasPrefix(lower, "gpt-") {
		return 0
	}
	return 1
}

// naturalLess 按段比较，数字段按数值，避免 gpt-5.10 排到 gpt-5.4 前面。
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			na, ni := readDigits(a, i)
			nb, nj := readDigits(b, j)
			if na != nb {
				return lessNumeric(na, nb)
			}
			i, j = ni, nj
			continue
		}
		if a[i] != b[j] {
			return a[i] < b[j]
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

// readDigits 返回去掉前导零的数字串及其结束位置。
func readDigits(text string, start int) (string, int) {
	end := start
	for end < len(text) && isDigit(text[end]) {
		end++
	}
	digits := strings.TrimLeft(text[start:end], "0")
	if digits == "" {
		digits = "0"
	}
	return digits, end
}

// lessNumeric 比较已去前导零的数字串，按位数再按字典序，长数字也不会溢出。
func lessNumeric(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// rememberModels 把一份结果记成「刚刚同步成功」，供预热和测试使用。
func (r *Runtime) rememberModels(models []string) {
	if r == nil || len(models) == 0 {
		return
	}
	r.modelsMu.Lock()
	r.modelsCache = append([]string(nil), models...)
	now := r.modelsClockLocked()
	r.modelsSyncedAt = now
	r.modelsTriedAt = now
	r.modelsMu.Unlock()
}

func (r *Runtime) cachedModels() []string {
	if r == nil {
		return nil
	}
	r.modelsMu.Lock()
	defer r.modelsMu.Unlock()
	return append([]string(nil), r.modelsCache...)
}

// directModels 直连本机 CPA 查模型列表。net/http 的 Timeout 由插件自己的
// goroutine 执行，所以这是唯一超时真正生效的来源。
//
// 第二个返回值表示 CPA 是否给出了应答。「CPA 答了一个空列表」和「压根问不到 CPA」
// 是两件事：前者说明它此刻确实没有可用的 gpt/codex 模型，后者才需要换个来源问。
func (r *Runtime) directModels() ([]string, bool) {
	port, apiKey := localCPAListen()
	if apiKey == "" {
		return nil, false
	}
	response, err := localModelsDirect(localModelsURL(port), apiKey)
	if err != nil || !host.IsHTTPSuccess(response.StatusCode) {
		return nil, false
	}
	return parseOpenAIModelIDs(response.Body), true
}

func localModelsURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port) + "/v1/models"
}

func localModelsDirect(url, apiKey string) (host.HTTPResponse, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return host.HTTPResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	client := &http.Client{
		Timeout:   modelsQueryTimeout,
		Transport: &http.Transport{Proxy: nil},
	}
	resp, err := client.Do(request)
	if err != nil {
		return host.HTTPResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return host.HTTPResponse{}, err
	}
	return host.HTTPResponse{StatusCode: resp.StatusCode, Body: body}, nil
}

func parseOpenAIModelIDs(raw []byte) []string {
	var document struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &document) != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, item := range document.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || seen[id] {
			continue
		}
		lower := strings.ToLower(id)
		owned := strings.ToLower(item.OwnedBy)
		if strings.Contains(lower, "image") || strings.Contains(lower, "video") || strings.Contains(lower, "imagine") {
			continue
		}
		openai := owned == "openai" || strings.Contains(lower, "gpt") || strings.Contains(lower, "codex")
		if !openai {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func localCPAListen() (int, string) {
	port := 8317
	apiKey := ""
	for _, path := range []string{"/CLIProxyAPI/config.yaml", "config.yaml"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		inKeys := false
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(trimmed, "port:") {
				if parsed, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "port:"))); err == nil && parsed > 0 {
					port = parsed
				}
				continue
			}
			if trimmed == "api-keys:" {
				inKeys = true
				continue
			}
			if inKeys {
				if strings.HasPrefix(trimmed, "-") {
					value := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
					value = strings.Trim(value, `"'`)
					if value != "" && apiKey == "" {
						apiKey = value
					}
					continue
				}
				if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
					inKeys = false
				}
			}
		}
		if apiKey != "" {
			return port, apiKey
		}
	}
	return port, apiKey
}
