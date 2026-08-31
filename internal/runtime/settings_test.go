package runtime

import (
	"path/filepath"
	"testing"
)

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	t.Setenv("QUOTA_SCHEDULE_REFRESH_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
	return New(nil)
}

func TestSaveSettingsRequiresSettingsField(t *testing.T) {
	r := newTestRuntime(t)
	before := r.config
	for _, body := range []string{`{}`, `{"settings":null}`, ``} {
		if _, err := r.saveSettings([]byte(body)); err == nil {
			t.Fatalf("body %q was accepted, expected an error", body)
		}
	}
	if r.config.ScheduleEnabled != before.ScheduleEnabled ||
		r.config.EnableDisabled != before.EnableDisabled ||
		r.config.SkipGPTPro != before.SkipGPTPro {
		t.Fatal("a rejected request must not change the running config")
	}
}

func TestSaveSettingsAppliesAndPersists(t *testing.T) {
	r := newTestRuntime(t)
	body := []byte(`{"settings":{"schedule_enabled":false,"daily_at":"09:30","timezone":"Asia/Shanghai",` +
		`"model":"gpt-5.6-sol","timeout_seconds":30,"enable_disabled":false,"skip_gpt_pro":false,` +
		`"max_concurrency":2,"retry_count":0,"retry_interval_seconds":0,"prompt":"ping"}}`)
	view, err := r.saveSettings(body)
	if err != nil {
		t.Fatalf("saveSettings: %v", err)
	}
	if !view.Stored {
		t.Fatal("settings were not written to disk")
	}
	if r.config.DailyAt != "09:30" || r.config.Model != "gpt-5.6-sol" || r.config.Prompt != "ping" {
		t.Fatalf("config not applied: %+v", r.config)
	}
	if r.config.EnableDisabled || r.config.SkipGPTPro {
		t.Fatal("toggles turned off in the form must reach the running config")
	}
	if r.config.RetryCount != 0 {
		t.Fatalf("retry_count = %d, want 0", r.config.RetryCount)
	}
}
