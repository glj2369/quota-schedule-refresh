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

func TestScheduleFieldsSurviveInlineComment(t *testing.T) {
	cfg, err := Parse([]byte("schedule_enabled: true # 开启定时\ndaily_at: 07:30 # 每天\nmax_concurrency: 2 # 并发\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.ScheduleEnabled || cfg.DailyAt != "07:30" || cfg.MaxConcurrency != 2 {
		t.Fatalf("got %+v", cfg)
	}
}
