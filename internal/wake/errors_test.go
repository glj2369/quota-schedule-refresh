package wake

import (
	"errors"
	"testing"
)

func TestFriendlyError(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "strips nested wrappers",
			raw:  "host callback host.model.execute: host_call_failed: auth_unavailable: no auth available (providers=codex, model=gpt-5.2-codex)",
			want: "auth_unavailable: no auth available (providers=codex, model=gpt-5.2-codex)",
		},
		{
			name: "json body keeps only the message",
			raw:  `host_call_failed: {&#34;error&#34;:{&#34;message&#34;:&#34;unknown provider for model no-such-model&#34;,&#34;code&#34;:&#34;model_not_found&#34;}}`,
			want: "unknown provider for model no-such-model",
		},
		{
			name: "empty falls back",
			raw:  "  host_call_failed:  ",
			want: "宿主模型执行失败",
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
	if got := friendlyHostError(errors.New("dial tcp 127.0.0.1:8317: connection refused")); got != "dial tcp 127.0.0.1:8317: connection refused" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestErrorDetailDecodesEntities(t *testing.T) {
	got := errorDetail(`{&#34;error&#34;:{&#34;message&#34;:&#34;boom&#34;}}`)
	if got != `{"error":{"message":"boom"}}` {
		t.Fatalf("unexpected detail: %q", got)
	}
}
