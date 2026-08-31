package config

import "testing"

func TestSkipGPTProDefaultsOn(t *testing.T) {
	if !Default().SkipGPTPro {
		t.Fatal("skip_gpt_pro 默认应为开启")
	}
	cfg, err := Parse([]byte("daily_at: \"08:00\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.SkipGPTPro {
		t.Fatal("配置缺省时应沿用默认开启")
	}
}

func TestSkipGPTProCanBeDisabled(t *testing.T) {
	for _, text := range []string{
		"skip_gpt_pro: false\n",
		"skip_gpt_pro: false # 同时刷新 Pro\n",
		"{\"skip_gpt_pro\": false}",
	} {
		cfg, err := Parse([]byte(text))
		if err != nil {
			t.Fatalf("parse %q: %v", text, err)
		}
		if cfg.SkipGPTPro {
			t.Fatalf("%q 未能关闭 skip_gpt_pro", text)
		}
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	base, err := Parse([]byte("daily_at: \"09:15\"\nmax_concurrency: 3\n"))
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	settings := ToSettings(base)
	if settings.DailyAt != "09:15" || settings.MaxConcurrency != 3 || settings.TimeoutSeconds != 60 {
		t.Fatalf("settings = %+v", settings)
	}
	settings.SkipGPTPro = false
	settings.RetryCount = 0
	applied, err := settings.Apply(base)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.SkipGPTPro || applied.RetryCount != 0 || applied.DailyAt != "09:15" || applied.MaxConcurrency != 3 {
		t.Fatalf("applied = %+v", applied)
	}
}

func TestSettingsBlankFieldsFallBackToBase(t *testing.T) {
	base := Default()
	base.Prompt = "custom"
	applied, err := Settings{}.Apply(base)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.DailyAt != base.DailyAt || applied.Timezone != base.Timezone {
		t.Fatalf("schedule fields not preserved: %+v", applied)
	}
	if applied.MaxConcurrency != base.MaxConcurrency || applied.Timeout != base.Timeout {
		t.Fatalf("numeric fields not preserved: %+v", applied)
	}
	if applied.Prompt != "custom" {
		t.Fatalf("prompt = %q", applied.Prompt)
	}
}

func TestApplyOverKeepsUnsetFields(t *testing.T) {
	base, err := Parse([]byte("daily_at: \"07:00\"\nprompt: \"ping\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	merged, err := ApplyOver(base, []byte(`{"schedule_enabled": true}`))
	if err != nil {
		t.Fatalf("apply over: %v", err)
	}
	if !merged.ScheduleEnabled || merged.DailyAt != "07:00" || merged.Prompt != "ping" {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestScheduleFieldsSurviveInlineComment(t *testing.T) {
	cfg, err := Parse([]byte("schedule_enabled: true # 开启定时\ndaily_at: 07:30 # 每天\nmax_concurrency: 2 # 并发\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.ScheduleEnabled || cfg.DailyAt != "07:30" || cfg.MaxConcurrency != 2 {
		t.Fatalf("got %+v", cfg)
	}
}
