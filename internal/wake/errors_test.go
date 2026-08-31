package wake

import (
	"errors"
	"testing"
)

func TestFriendlyErrorStripsWrappersAndMapsHints(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "auth unavailable",
			raw:  "host callback host.model.execute: host_call_failed: auth_unavailable: no auth available (providers=codex, model=gpt-5.2-codex)",
			want: "CPA 没有可用凭证，可能已被禁用或全部限流",
		},
		{
			name: "timeout",
			raw:  "host.model.execute: context deadline exceeded",
			want: "请求超时",
		},
		{
			name: "rate limit",
			raw:  "host_call_failed: HTTP 429 too many requests",
			want: "触发限流，稍后重试",
		},
		{
			name: "unknown keeps stripped text",
			raw:  "host_call_failed: upstream exploded (providers=codex)",
			want: "upstream exploded",
		},
		{
			name: "empty falls back",
			raw:  "  host_call_failed:  ",
			want: "宿主模型执行失败",
		},
		{
			name: "json error body keeps only the message",
			raw:  `host_call_failed: {&#34;error&#34;:{&#34;message&#34;:&#34;upstream is having a bad day&#34;,&#34;type&#34;:&#34;server_error&#34;}}`,
			want: "upstream is having a bad day",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := friendlyError(tc.raw); got != tc.want {
				t.Fatalf("friendlyError(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestFriendlyHostErrorNil(t *testing.T) {
	if got := friendlyHostError(nil); got != "宿主模型执行失败" {
		t.Fatalf("unexpected fallback: %q", got)
	}
	if got := friendlyHostError(errors.New("dial tcp 127.0.0.1:8317: connection refused")); got != "无法连接 CPA 接口" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestStripTrailingContextKeepsPlainParens(t *testing.T) {
	if got := stripTrailingContext("模型返回了空内容（重试无效）"); got != "模型返回了空内容（重试无效）" {
		t.Fatalf("unexpected strip: %q", got)
	}
	if got := stripTrailingContext("boom (providers=codex, model=x)"); got != "boom" {
		t.Fatalf("unexpected strip: %q", got)
	}
}
