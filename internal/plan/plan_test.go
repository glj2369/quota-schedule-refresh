package plan

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestFromFilenamePlusAndPro(t *testing.T) {
	plus := FromAuth([]string{"codex-056fd6fc-glj2369@proton.me-plus.json"}, nil)
	if plus != "plus" {
		t.Fatalf("plus = %q", plus)
	}
	pro := FromAuth([]string{"codex-abc-user@gmail.com-pro.json"}, nil)
	if !IsGPTPro(pro) {
		t.Fatalf("pro = %q", pro)
	}
}

func TestProtonEmailIsNotPro(t *testing.T) {
	plan := FromAuth([]string{"codex-056fd6fc-glj2369@proton.me.json"}, nil)
	if IsGPTPro(plan) || plan == "pro" {
		t.Fatalf("got %q", plan)
	}
}

func TestProLiteIsNotGPTPro(t *testing.T) {
	plan := FromAuth([]string{"codex-user@x.com-prolite.json"}, nil)
	if plan != "prolite" || IsGPTPro(plan) {
		t.Fatalf("prolite treated as pro: %q", plan)
	}
	if !SkipOnSchedule(plan) {
		t.Fatal("prolite should skip on schedule like pro")
	}
}

func TestLiveProLiteBeatsPlusFilename(t *testing.T) {
	token := fakeJWT(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type": "prolite",
		},
	})
	body, _ := json.Marshal(map[string]string{"id_token": token, "email": "a@b.com"})
	plan := FromAuth([]string{"codex-056fd6fc-glj2369@proton.me-plus.json"}, body)
	if plan != "prolite" {
		t.Fatalf("live plan = %q, want prolite over plus filename", plan)
	}
}

func TestPlanFromJWT(t *testing.T) {
	token := fakeJWT(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type": "pro",
		},
	})
	body, _ := json.Marshal(map[string]string{"id_token": token, "email": "a@b.com"})
	plan := FromAuth([]string{"codex-a@b.com.json"}, body)
	if !IsGPTPro(plan) {
		t.Fatalf("jwt plan = %q", plan)
	}
}

func TestPlanFromPlainField(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"email": "a@b.com", "plan": "pro"})
	if plan := FromAuth([]string{"codex-a@b.com.json"}, body); !IsGPTPro(plan) {
		t.Fatalf("plain plan field = %q", plan)
	}
	nested, _ := json.Marshal(map[string]any{"account": map[string]any{"plan_type": "Plus"}})
	if plan := FromAuth([]string{"codex-a@b.com.json"}, nested); plan != "plus" {
		t.Fatalf("nested plan field = %q", plan)
	}
}

func TestPlanFromMetadataExtras(t *testing.T) {
	extras := map[string]any{"chatgpt_plan_type": "PRO"}
	if plan := FromAuth([]string{"codex-a@b.com.json"}, nil, extras); !IsGPTPro(plan) {
		t.Fatalf("metadata plan = %q", plan)
	}
}

func TestUnknownPlanIsNotPro(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"plan": "something-else"})
	if plan := FromAuth([]string{"codex-a@b.com.json"}, body); IsGPTPro(plan) {
		t.Fatalf("unknown plan treated as pro: %q", plan)
	}
}

func TestSkipOnScheduleCoversProAndFree(t *testing.T) {
	free := FromAuth([]string{"codex-user@x.com-free.json"}, nil)
	if free != "free" || !SkipOnSchedule(free) {
		t.Fatalf("free = %q skip=%v", free, SkipOnSchedule(free))
	}
	pro := FromAuth([]string{"codex-user@x.com-pro.json"}, nil)
	if !SkipOnSchedule(pro) {
		t.Fatalf("pro should skip")
	}
	plus := FromAuth([]string{"codex-user@x.com-plus.json"}, nil)
	if SkipOnSchedule(plus) {
		t.Fatalf("plus should still refresh, got skip for %q", plus)
	}
	if SkipReason("pro") != "GPT Pro，已跳过" || SkipReason("free") != "Free，已跳过" || SkipReason("prolite") != "Pro Lite，已跳过" {
		t.Fatalf("SkipReason pro=%q free=%q lite=%q", SkipReason("pro"), SkipReason("free"), SkipReason("prolite"))
	}
}

func fakeJWT(payload map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	raw, _ := json.Marshal(payload)
	body := base64.RawURLEncoding.EncodeToString(raw)
	return header + "." + body + ".sig"
}
