package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quota-schedule-refresh/internal/config"
	"quota-schedule-refresh/internal/host"
)

// fakeHost 是一台什么都答不上来的宿主：凭证列得出来，但模型一个都查不到。
// 这正是「彻底没有可用模型」的场景。
type fakeHost struct {
	files     []host.AuthFile
	executed  atomic.Int64
	lastModel atomic.Value
}

func (f *fakeHost) ModelExecute(_ context.Context, request host.ModelExecuteRequest) (host.ModelExecuteResponse, error) {
	f.executed.Add(1)
	f.lastModel.Store(request.Model)
	return host.ModelExecuteResponse{StatusCode: 200, Body: []byte(`{"choices":[{"message":{"content":"ok"}}]}`)}, nil
}

func (f *fakeHost) HTTPDo(_ context.Context, _ host.HTTPRequest) (host.HTTPResponse, error) {
	return host.HTTPResponse{}, errors.New("no http from the fake host")
}

func (f *fakeHost) ListAuthFiles(_ context.Context) ([]host.AuthFile, error) {
	return f.files, nil
}

func (f *fakeHost) GetAuthFile(_ context.Context, key string) (host.AuthFile, error) {
	for _, file := range f.files {
		if file.ID == key || file.Name == key || file.AuthIndex == key {
			cloned := file
			cloned.Data = append([]byte(nil), file.Data...)
			return cloned, nil
		}
	}
	return host.AuthFile{}, errors.New("not available")
}

func (f *fakeHost) GetRuntimeAuthFile(_ context.Context, _ string) (host.AuthFile, error) {
	return host.AuthFile{}, errors.New("not available")
}

func (f *fakeHost) SaveAuthFile(_ context.Context, name string, data []byte) error {
	for i, file := range f.files {
		if file.Name == name || file.ID == name {
			f.files[i].Data = append([]byte(nil), data...)
			return nil
		}
	}
	f.files = append(f.files, host.AuthFile{Name: name, Data: append([]byte(nil), data...)})
	return nil
}

// newHostRuntime 把工作目录换到一个没有 config.yaml 的临时目录，
// 这样 localCPAListen 找不到 api-key，/v1/models 那条来源自然为空。
func newHostRuntime(t *testing.T, client host.Client) *Runtime {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("QUOTA_SCHEDULE_REFRESH_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
	return New(client)
}

func TestChooseModelPrefersConfiguredThenHighestVersion(t *testing.T) {
	available := []string{"gpt-5.3-codex-spark", "gpt-5.6-luna", "gpt-5.4"}
	if got := chooseModel(" gpt-5.6-sol ", available, "gpt-5.4"); got != "gpt-5.6-sol" {
		t.Fatalf("chooseModel with a configured model = %q, want gpt-5.6-sol", got)
	}
	if got := chooseModel("", available, "gpt-5.4"); got != "gpt-5.6-luna" {
		t.Fatalf("chooseModel = %q, want the highest version gpt-5.6-luna", got)
	}
	if got := chooseModel("", nil, " gpt-5.4 "); got != "gpt-5.4" {
		t.Fatalf("chooseModel with only a fallback = %q, want gpt-5.4", got)
	}
}

// TestChooseModelReturnsEmptyWhenNothingIsAvailable 锁住去掉硬编码 gpt-5-mini 的效果：
// 没有任何来源时必须诚实地返回空，而不是猜一个可能已经下线的名字。
func TestChooseModelReturnsEmptyWhenNothingIsAvailable(t *testing.T) {
	if got := chooseModel("", nil, ""); got != "" {
		t.Fatalf("chooseModel with no sources = %q, want an empty string", got)
	}
	if got := chooseModel("  ", []string{}, "  "); got != "" {
		t.Fatalf("chooseModel with blank sources = %q, want an empty string", got)
	}
}

// TestActivateAllReportsMissingModel 是改动 B 的行为测试：一个模型都拿不到时，
// 执行记录里要写「没有可用模型」，而且不能真的对宿主发一次注定失败的请求。
func TestActivateAllReportsMissingModel(t *testing.T) {
	client := &fakeHost{files: []host.AuthFile{{
		ID:       "codex-1",
		Name:     "codex-account",
		Provider: "codex",
		Account:  "someone@example.com",
	}}}
	r := newHostRuntime(t, client)

	cfg := config.Default()
	cfg.Model = ""
	cfg.SkipGPTPro = false

	results, summary := r.activateAll(context.Background(), cfg, nil)
	if len(results) != 1 {
		t.Fatalf("activateAll returned %d results, want 1: %+v", len(results), results)
	}
	got := results[0]
	if got.Success || got.Status != "failed" {
		t.Fatalf("result = %+v, want a failure", got)
	}
	if !strings.Contains(got.LastError, "没有可用模型") {
		t.Fatalf("LastError = %q, want it to name the missing model list", got.LastError)
	}
	if strings.Contains(got.LastError, "gpt-5-mini") || got.Model == "gpt-5-mini" {
		t.Fatalf("result still mentions the hardcoded model: %+v", got)
	}
	if n := client.executed.Load(); n != 0 {
		t.Fatalf("sent %d model executions, want 0 when there is no model to send", n)
	}
	if !strings.Contains(summary, "失败 1") {
		t.Fatalf("summary = %q, want it to count the credential as failed", summary)
	}
}

func TestActivateAllSkipsProAndFreeOnSchedule(t *testing.T) {
	tokenDoc := []byte(`{"access_token":"` + strings.Repeat("x", 40) + `","refresh_token":"` + strings.Repeat("y", 40) + `","type":"codex"}`)
	client := &fakeHost{files: []host.AuthFile{
		{ID: "plus-1", Name: "codex-a-plus.json", Provider: "codex", Models: []string{"gpt-5.4"}, Data: tokenDoc},
		{ID: "pro-1", Name: "codex-b-pro.json", Provider: "codex", Models: []string{"gpt-5.4"}, Data: tokenDoc},
		{ID: "lite-1", Name: "codex-d-prolite.json", Provider: "codex", Models: []string{"gpt-5.4"}, Data: tokenDoc},
		{ID: "free-1", Name: "codex-c-free.json", Provider: "codex", Models: []string{"gpt-5.4"}, Data: tokenDoc},
	}}
	r := newHostRuntime(t, client)
	cfg := config.Default()
	cfg.Model = "gpt-5.4"
	cfg.SkipGPTPro = true
	cfg.RetryCount = 0

	results, summary := r.activateAll(context.Background(), cfg, nil)
	if client.executed.Load() != 1 {
		t.Fatalf("executed %d, want only the plus account", client.executed.Load())
	}
	skipped := map[string]string{}
	ran := 0
	for _, item := range results {
		if item.Status == "skipped" {
			skipped[item.AuthID] = item.Reply
			continue
		}
		ran++
	}
	if ran != 1 {
		t.Fatalf("ran %d accounts, want 1: %+v", ran, results)
	}
	if skipped["pro-1"] != "GPT Pro，已跳过" {
		t.Fatalf("pro skip = %q", skipped["pro-1"])
	}
	if skipped["lite-1"] != "Pro Lite，已跳过" {
		t.Fatalf("prolite skip = %q", skipped["lite-1"])
	}
	if skipped["free-1"] != "Free，已跳过" {
		t.Fatalf("free skip = %q", skipped["free-1"])
	}
	if _, ok := skipped["plus-1"]; ok {
		t.Fatalf("plus was skipped: %+v", results)
	}
	if !strings.Contains(summary, "跳过 3") {
		t.Fatalf("summary = %q", summary)
	}

	client.executed.Store(0)
	picked, _ := r.activateAll(context.Background(), cfg, []string{"free-1"})
	if client.executed.Load() != 1 {
		t.Fatalf("manual free refresh executed %d, want 1", client.executed.Load())
	}
	if len(picked) != 1 || picked[0].Status == "skipped" {
		t.Fatalf("manual free refresh = %+v, want it to run", picked)
	}
}

// TestActivateAllUsesHighestVersionFromCredential 确认凭证自带模型列表时，
// 回落同样挑版本最高的，而不是列表里的第一个。
func TestActivateAllUsesHighestVersionFromCredential(t *testing.T) {
	client := &fakeHost{files: []host.AuthFile{{
		ID:       "codex-1",
		Name:     "codex-account",
		Provider: "codex",
		Models:   []string{"gpt-5.3-codex-spark", "gpt-5.6-luna", "gpt-5.4"},
	}}}
	r := newHostRuntime(t, client)

	cfg := config.Default()
	cfg.Model = ""
	cfg.SkipGPTPro = false
	cfg.RetryCount = 0

	results, _ := r.activateAll(context.Background(), cfg, nil)
	if len(results) != 1 {
		t.Fatalf("activateAll returned %d results, want 1", len(results))
	}
	if results[0].Model != "gpt-5.6-luna" {
		t.Fatalf("used model %q, want the highest version gpt-5.6-luna", results[0].Model)
	}
	if got, _ := client.lastModel.Load().(string); got != "gpt-5.6-luna" {
		t.Fatalf("host was asked for model %q, want gpt-5.6-luna", got)
	}
}

// TestDefaultModelIsEmptyWithoutAnySource 确认 /status 的 default_model 在
// 查不到模型时是空的：页面会显示 "-"，比显示一个不存在的名字诚实。
func TestDefaultModelIsEmptyWithoutAnySource(t *testing.T) {
	r := newHostRuntime(t, &fakeHost{})
	if got := r.defaultModel(); got != "" {
		t.Fatalf("defaultModel() = %q, want an empty string", got)
	}
}

func TestSkipMissedScheduleAfterReload(t *testing.T) {
	r := newHostRuntime(t, &fakeHost{})
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 16, 0, 28, 0, loc)
	r.now = func() time.Time { return now }
	r.config.DailyAt = "08:00"
	r.config.Timezone = "Asia/Shanghai"

	if wait := r.preScanWait(); wait != 0 {
		t.Fatalf("wait = %s, want 0 after today's 08:00", wait)
	}
	if !r.skipMissedSchedule() {
		t.Fatal("reload after 08:00 should skip catch-up")
	}
	wait := r.preScanWait()
	next := now.Add(wait)
	want := time.Date(2026, 9, 2, 8, 0, 0, 0, loc)
	if next.Sub(want) > time.Second || want.Sub(next) > time.Second {
		t.Fatalf("next scan = %s, want %s", next, want)
	}
}

func TestScheduleStillWaitsBeforeTrigger(t *testing.T) {
	r := newHostRuntime(t, &fakeHost{})
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 7, 0, 0, 0, loc)
	r.now = func() time.Time { return now }
	r.config.DailyAt = "08:00"
	r.config.Timezone = "Asia/Shanghai"

	wait := r.preScanWait()
	if wait != time.Hour {
		t.Fatalf("wait = %s, want 1h until 08:00", wait)
	}
	if r.skipMissedSchedule() {
		t.Fatal("before 08:00 should not consume the day's trigger")
	}
}
