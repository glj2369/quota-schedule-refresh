package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidConfig = errors.New("配置无效")

const DefaultTimezone = "Asia/Shanghai"

type Config struct {
	ScheduleEnabled bool
	DailyAt         string
	Timezone        string
	Model           string
	Timeout         time.Duration
	Prompt          string
	EnableDisabled  bool
	SkipGPTPro      bool
	RetryCount      int
	RetryInterval   time.Duration
	DailyHour       int
	DailyMinute     int
}

func Default() Config {
	return Config{
		ScheduleEnabled: false,
		DailyAt:         "08:00",
		Timezone:        DefaultTimezone,
		Model:           "",
		Timeout:         time.Minute,
		Prompt:          "hello",
		EnableDisabled:  true,
		SkipGPTPro:      true,
		RetryCount:      2,
		RetryInterval:   2 * time.Second,
		DailyHour:       8,
		DailyMinute:     0,
	}
}

type rawConfig struct {
	ScheduleEnabled *bool   `json:"schedule_enabled"`
	DailyAt         *string `json:"daily_at"`
	Timezone        *string `json:"timezone"`
	Model           *string `json:"model"`
	TimeoutSeconds  any     `json:"timeout_seconds"`
	Prompt          *string `json:"prompt"`
	EnableDisabled  *bool   `json:"enable_disabled"`
	SkipGPTPro      *bool   `json:"skip_gpt_pro"`
	RetryCount      any     `json:"retry_count"`
	RetryInterval   any     `json:"retry_interval_seconds"`
}

func Parse(data []byte) (Config, error) {
	return ApplyOver(Default(), data)
}

// ApplyOver 在基线配置上叠加一层覆盖，供插件私有设置覆盖宿主 config.yaml。
func ApplyOver(base Config, data []byte) (Config, error) {
	raw, err := decodeRaw(data)
	if err != nil {
		return Config{}, err
	}
	return raw.apply(base)
}

// Settings 是设置页面与私有配置文件共用的字段集，键名与 config.yaml 保持一致。
// 旧文件里的 max_concurrency 会被忽略：刷新必须串行，见 wake.withBoostedAuth。
type Settings struct {
	ScheduleEnabled bool   `json:"schedule_enabled"`
	DailyAt         string `json:"daily_at"`
	Timezone        string `json:"timezone"`
	Model           string `json:"model"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	EnableDisabled  bool   `json:"enable_disabled"`
	SkipGPTPro      bool   `json:"skip_gpt_pro"`
	RetryCount      int    `json:"retry_count"`
	RetryInterval   int    `json:"retry_interval_seconds"`
	Prompt          string `json:"prompt"`
}

func ToSettings(cfg Config) Settings {
	return Settings{
		ScheduleEnabled: cfg.ScheduleEnabled,
		DailyAt:         cfg.DailyAt,
		Timezone:        cfg.Timezone,
		Model:           cfg.Model,
		TimeoutSeconds:  int(cfg.Timeout / time.Second),
		EnableDisabled:  cfg.EnableDisabled,
		SkipGPTPro:      cfg.SkipGPTPro,
		RetryCount:      cfg.RetryCount,
		RetryInterval:   int(cfg.RetryInterval / time.Second),
		Prompt:          cfg.Prompt,
	}
}

// Apply 校验设置并生成配置，未填写的项沿用 base。
// retry_count 与 retry_interval_seconds 的 0 是有效值，不做留空回填。
func (s Settings) Apply(base Config) (Config, error) {
	if s.TimeoutSeconds <= 0 {
		s.TimeoutSeconds = int(base.Timeout / time.Second)
	}
	if strings.TrimSpace(s.DailyAt) == "" {
		s.DailyAt = base.DailyAt
	}
	if strings.TrimSpace(s.Timezone) == "" {
		s.Timezone = base.Timezone
	}
	data, err := json.Marshal(s)
	if err != nil {
		return Config{}, fmt.Errorf("%w: 设置序列化失败", ErrInvalidConfig)
	}
	return ApplyOver(base, data)
}

func (s Settings) Encode() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

func decodeRaw(data []byte) (rawConfig, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return rawConfig{}, nil
	}
	if trimmed[0] == '{' {
		var raw rawConfig
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			return rawConfig{}, fmt.Errorf("%w: 配置 JSON 无效", ErrInvalidConfig)
		}
		return raw, nil
	}
	mapped := map[string]any{}
	for _, line := range strings.Split(trimmed, "\n") {
		item := strings.TrimSpace(line)
		if item == "" || strings.HasPrefix(item, "#") || strings.HasPrefix(item, "-") {
			continue
		}
		key, value, ok := strings.Cut(item, ":")
		if !ok {
			continue
		}
		mapped[strings.TrimSpace(key)] = yamlScalar(strings.TrimSpace(value))
	}
	encoded, err := json.Marshal(mapped)
	if err != nil {
		return rawConfig{}, fmt.Errorf("%w: YAML 编码失败", ErrInvalidConfig)
	}
	var raw rawConfig
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return rawConfig{}, fmt.Errorf("%w: YAML 不匹配配置结构", ErrInvalidConfig)
	}
	return raw, nil
}

func yamlScalar(value string) any {
	text := strings.TrimSpace(value)
	if len(text) >= 2 && text[0] == text[len(text)-1] && (text[0] == '"' || text[0] == '\'') {
		text = text[1 : len(text)-1]
	} else if i := strings.Index(text, " #"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	if text == "" {
		return nil
	}
	if parsed, err := strconv.Atoi(text); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseBool(text); err == nil {
		return parsed
	}
	return text
}

func (raw rawConfig) apply(cfg Config) (Config, error) {
	if raw.ScheduleEnabled != nil {
		cfg.ScheduleEnabled = *raw.ScheduleEnabled
	}
	if raw.DailyAt != nil {
		cfg.DailyAt = strings.TrimSpace(*raw.DailyAt)
	}
	if raw.Timezone != nil {
		cfg.Timezone = strings.TrimSpace(*raw.Timezone)
	}
	if raw.Model != nil {
		cfg.Model = strings.TrimSpace(*raw.Model)
	}
	if raw.Prompt != nil && strings.TrimSpace(*raw.Prompt) != "" {
		cfg.Prompt = strings.TrimSpace(*raw.Prompt)
	}
	if raw.EnableDisabled != nil {
		cfg.EnableDisabled = *raw.EnableDisabled
	}
	if raw.SkipGPTPro != nil {
		cfg.SkipGPTPro = *raw.SkipGPTPro
	}
	if raw.RetryCount != nil {
		parsed, err := parseBoundedInt(raw.RetryCount, 0, 10, "retry_count")
		if err != nil {
			return Config{}, err
		}
		cfg.RetryCount = parsed
	}
	if raw.RetryInterval != nil {
		parsed, err := parseNonNegativeSeconds(raw.RetryInterval, "retry_interval_seconds")
		if err != nil {
			return Config{}, err
		}
		if parsed > 30*time.Second {
			parsed = 30 * time.Second
		}
		cfg.RetryInterval = parsed
	}
	if raw.TimeoutSeconds != nil {
		parsed, err := parseTimeoutAny(raw.TimeoutSeconds)
		if err != nil {
			return Config{}, err
		}
		if parsed > 0 {
			cfg.Timeout = parsed
		}
	}
	if cfg.DailyAt == "" {
		cfg.DailyAt = "08:00"
	}
	hour, minute, err := parseClock(cfg.DailyAt)
	if err != nil {
		return Config{}, err
	}
	cfg.DailyHour = hour
	cfg.DailyMinute = minute
	cfg.DailyAt = fmt.Sprintf("%02d:%02d", hour, minute)
	if cfg.Timezone == "" {
		cfg.Timezone = DefaultTimezone
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return Config{}, fmt.Errorf("%w: timezone 无效", ErrInvalidConfig)
	}
	return cfg, nil
}

func parseClock(raw string) (int, int, error) {
	text := strings.TrimSpace(raw)
	parts := strings.Split(text, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("%w: daily_at 必须是 HH:MM", ErrInvalidConfig)
	}
	hour, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("%w: daily_at 小时无效", ErrInvalidConfig)
	}
	minute, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("%w: daily_at 分钟无效", ErrInvalidConfig)
	}
	return hour, minute, nil
}

func parseTimeoutAny(raw any) (time.Duration, error) {
	switch value := raw.(type) {
	case nil:
		return 0, nil
	case string:
		return parseTimeout(value)
	case float64:
		if value <= 0 {
			return 0, fmt.Errorf("%w: timeout_seconds 必须大于 0", ErrInvalidConfig)
		}
		return time.Duration(value) * time.Second, nil
	case json.Number:
		n, err := value.Float64()
		if err != nil {
			return 0, fmt.Errorf("%w: timeout_seconds 无效", ErrInvalidConfig)
		}
		return parseTimeoutAny(n)
	default:
		return parseTimeout(fmt.Sprint(value))
	}
}

func parseBoundedInt(raw any, min, max int, field string) (int, error) {
	switch value := raw.(type) {
	case nil:
		return 0, nil
	case int:
		if value < min || value > max {
			return 0, fmt.Errorf("%w: %s 必须在 %d 到 %d 之间", ErrInvalidConfig, field, min, max)
		}
		return value, nil
	case float64:
		return parseBoundedInt(int(value), min, max, field)
	case json.Number:
		n, err := value.Int64()
		if err != nil {
			return 0, fmt.Errorf("%w: %s 无效", ErrInvalidConfig, field)
		}
		return parseBoundedInt(int(n), min, max, field)
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0, fmt.Errorf("%w: %s 无效", ErrInvalidConfig, field)
		}
		return parseBoundedInt(n, min, max, field)
	default:
		return parseBoundedInt(fmt.Sprint(value), min, max, field)
	}
}

func parseNonNegativeSeconds(raw any, field string) (time.Duration, error) {
	switch value := raw.(type) {
	case nil:
		return 0, nil
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0, fmt.Errorf("%w: %s 无效", ErrInvalidConfig, field)
		}
		if n < 0 {
			return 0, fmt.Errorf("%w: %s 不能为负数", ErrInvalidConfig, field)
		}
		return time.Duration(n) * time.Second, nil
	case float64:
		if value < 0 {
			return 0, fmt.Errorf("%w: %s 不能为负数", ErrInvalidConfig, field)
		}
		return time.Duration(value) * time.Second, nil
	case json.Number:
		n, err := value.Float64()
		if err != nil {
			return 0, fmt.Errorf("%w: %s 无效", ErrInvalidConfig, field)
		}
		return parseNonNegativeSeconds(n, field)
	default:
		return parseNonNegativeSeconds(fmt.Sprint(value), field)
	}
}

func parseTimeout(raw string) (time.Duration, error) {
	text := strings.TrimSpace(raw)
	if _, err := time.ParseDuration(text); err != nil && strings.Contains(err.Error(), "missing unit") {
		text += "s"
	}
	parsed, err := time.ParseDuration(text)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%w: timeout_seconds 必须大于 0", ErrInvalidConfig)
	}
	return parsed, nil
}

func (c Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}

func (c Config) TodayTrigger(now time.Time) time.Time {
	local := now.In(c.Location())
	return time.Date(local.Year(), local.Month(), local.Day(), c.DailyHour, c.DailyMinute, 0, 0, c.Location())
}

func (c Config) NextTrigger(now time.Time) time.Time {
	today := c.TodayTrigger(now)
	if now.Before(today) {
		return today
	}
	return today.AddDate(0, 0, 1)
}

func (c Config) WaitUntilScan(now, lastScan time.Time) time.Duration {
	today := c.TodayTrigger(now)
	loc := c.Location()
	lastLocal := lastScan.In(loc)
	ranToday := !lastScan.IsZero() && !lastLocal.Before(today)
	if ranToday {
		wait := c.NextTrigger(now).Sub(now)
		if wait < 0 {
			return 0
		}
		return wait
	}
	if now.Before(today) {
		return today.Sub(now)
	}
	return 0
}
