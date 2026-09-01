package plan

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode"
)

var knownPlans = map[string]string{
	"free":       "free",
	"plus":       "plus",
	"pro":        "pro",
	"team":       "team",
	"business":   "business",
	"enterprise": "enterprise",
	"edu":        "edu",
	"go":         "go",
	"student":    "student",
	"prolite":    "prolite",
}

var planKeys = []string{
	"chatgpt_plan_type",
	"chatgptPlanType",
	"plan_type",
	"planType",
	"plan",
	"subscription_plan",
}

func FromAuth(names []string, data []byte, extras ...map[string]any) string {
	for _, name := range names {
		if plan := fromFilename(name); plan != "" {
			return plan
		}
	}
	for _, extra := range extras {
		if plan := fromValue(extra); plan != "" {
			return plan
		}
	}
	return fromJSON(data)
}

func IsGPTPro(plan string) bool {
	return Normalize(plan) == "pro"
}

// SkipOnSchedule is true for plans the skip_gpt_pro toggle also leaves out of
// a scheduled run: GPT Pro and Free. Plus / team / the rest still refresh.
func SkipOnSchedule(plan string) bool {
	switch Normalize(plan) {
	case "pro", "free":
		return true
	default:
		return false
	}
}

func SkipReason(plan string) string {
	switch Normalize(plan) {
	case "pro":
		return "GPT Pro，已跳过"
	case "free":
		return "Free，已跳过"
	default:
		return "已跳过"
	}
}

func Normalize(plan string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(plan)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	text := b.String()
	if mapped, ok := knownPlans[text]; ok {
		return mapped
	}
	return text
}

func fromFilename(name string) string {
	text := strings.TrimSpace(name)
	text = strings.ReplaceAll(text, "\\", "/")
	if i := strings.LastIndex(text, "/"); i >= 0 {
		text = text[i+1:]
	}
	text = strings.TrimSuffix(strings.ToLower(text), ".json")
	if text == "" {
		return ""
	}
	parts := strings.Split(text, "-")
	if len(parts) == 0 {
		return ""
	}
	plan := Normalize(parts[len(parts)-1])
	if _, ok := knownPlans[plan]; ok {
		return plan
	}
	return ""
}

func fromJSON(data []byte) string {
	if len(strings.TrimSpace(string(data))) == 0 {
		return ""
	}
	var raw any
	if json.Unmarshal(data, &raw) != nil {
		return ""
	}
	return fromValue(raw)
}

func fromValue(value any) string {
	return valuePlan(value, 0)
}

func valuePlan(value any, depth int) string {
	if depth > 6 {
		return ""
	}
	switch item := value.(type) {
	case string:
		return planFromJWT(item)
	case map[string]any:
		for _, key := range planKeys {
			if plan := knownPlans[Normalize(asString(item[key]))]; plan != "" {
				return plan
			}
		}
		for _, key := range []string{"id_token", "idToken", "access_token", "accessToken"} {
			if plan := planFromJWT(asString(item[key])); plan != "" {
				return plan
			}
		}
		for _, nested := range item {
			if plan := valuePlan(nested, depth+1); plan != "" {
				return plan
			}
		}
	case []any:
		for _, nested := range item {
			if plan := valuePlan(nested, depth+1); plan != "" {
				return plan
			}
		}
	}
	return ""
}

func planFromJWT(token string) string {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		padded := parts[1]
		if n := len(padded) % 4; n != 0 {
			padded += strings.Repeat("=", 4-n)
		}
		payload, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return ""
		}
	}
	var raw any
	if json.Unmarshal(payload, &raw) != nil {
		return ""
	}
	return jwtPlan(raw, 0)
}

func jwtPlan(value any, depth int) string {
	if depth > 6 {
		return ""
	}
	switch item := value.(type) {
	case map[string]any:
		for _, key := range planKeys {
			if plan := knownPlans[Normalize(asString(item[key]))]; plan != "" {
				return plan
			}
		}
		for _, nested := range item {
			if plan := jwtPlan(nested, depth+1); plan != "" {
				return plan
			}
		}
	case []any:
		for _, nested := range item {
			if plan := jwtPlan(nested, depth+1); plan != "" {
				return plan
			}
		}
	}
	return ""
}

func asString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
