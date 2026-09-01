package runtime

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testClock 让 TTL 可以被测试推进。用原子量是因为后台刷新的 goroutine 也会读它。
type testClock struct {
	nanos atomic.Int64
}

func newTestClock() *testClock {
	clock := &testClock{}
	clock.nanos.Store(time.Now().UnixNano())
	return clock
}

func (c *testClock) now() time.Time {
	return time.Unix(0, c.nanos.Load())
}

func (c *testClock) advance(d time.Duration) {
	c.nanos.Add(int64(d))
}

// modelsResponse 模仿 CPA /v1/models 的返回：混着 grok 和图像模型，顺序也是乱的。
func modelsResponse(ids ...string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		owner := "openai"
		if strings.HasPrefix(id, "grok") {
			owner = "xai"
		}
		items = append(items, fmt.Sprintf(`{"id":%q,"owned_by":%q}`, id, owner))
	}
	return `{"object":"list","data":[` + strings.Join(items, ",") + `]}`
}

// modelsProbe 读出「上次发起查询的时刻」和在途标记。这两个值都是在持锁时同步写入的，
// 因此比观察服务端计数可靠：后台刷新的请求可能还没到达服务端。
func modelsProbe(t *testing.T, r *Runtime) (time.Time, chan struct{}) {
	t.Helper()
	r.modelsMu.Lock()
	defer r.modelsMu.Unlock()
	return r.modelsTriedAt, r.modelsInflight
}

// assertNoQueryStarted 确认这期间一次查询都没发起过。
func assertNoQueryStarted(t *testing.T, r *Runtime, before time.Time, hits *atomic.Int64, wantHits int64) {
	t.Helper()
	triedAt, inflight := modelsProbe(t, r)
	if !triedAt.Equal(before) {
		t.Fatalf("a cache hit still started a query: modelsTriedAt moved %v -> %v", before, triedAt)
	}
	if inflight != nil {
		t.Fatal("a cache hit still started a background refresh")
	}
	// 再给后台一点时间，确保没有请求在路上。
	time.Sleep(200 * time.Millisecond)
	if got := hits.Load(); got != wantHits {
		t.Fatalf("sent %d requests, want %d", got, wantHits)
	}
}

// newModelsRuntime 起一个假的本机 CPA，并写好 config.yaml 让 localCPAListen 找到它。
func newModelsRuntime(t *testing.T, handler http.HandlerFunc) *Runtime {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("split server url %q: %v", server.URL, err)
	}

	dir := t.TempDir()
	yaml := fmt.Sprintf("port: %s\napi-keys:\n  - test-key\n", port)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Chdir(dir)
	t.Setenv("QUOTA_SCHEDULE_REFRESH_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
	return New(nil)
}

func TestAvailableModelsServesFreshCacheWithoutQuerying(t *testing.T) {
	var hits atomic.Int64
	r := newModelsRuntime(t, func(w http.ResponseWriter, request *http.Request) {
		hits.Add(1)
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want the api-key from config.yaml", got)
		}
		_, _ = w.Write([]byte(modelsResponse(
			"gpt-5.6-luna", "gpt-image-2", "grok-4.6", "gpt-5.4", "gpt-5.5", "codex-auto-review", "gpt-5.4-mini",
		)))
	})

	want := []string{"gpt-5.4", "gpt-5.4-mini", "gpt-5.5", "gpt-5.6-luna", "codex-auto-review"}
	if got := r.availableModels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("cold start availableModels() = %v, want %v", got, want)
	}
	if hits.Load() != 1 {
		t.Fatalf("cold start sent %d requests, want 1", hits.Load())
	}

	// TTL 之内的调用必须完全不发请求。
	triedBefore, _ := modelsProbe(t, r)
	for i := 0; i < 5; i++ {
		if got := r.availableModels(); !reflect.DeepEqual(got, want) {
			t.Fatalf("cached availableModels() = %v, want %v", got, want)
		}
	}
	assertNoQueryStarted(t, r, triedBefore, &hits, 1)
}

func TestAvailableModelsRefreshesAfterTTL(t *testing.T) {
	var hits atomic.Int64
	r := newModelsRuntime(t, func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			_, _ = w.Write([]byte(modelsResponse("gpt-5.4")))
			return
		}
		_, _ = w.Write([]byte(modelsResponse("gpt-5.5")))
	})

	clock := newTestClock()
	r.modelsNow = clock.now

	if got := r.availableModels(); !reflect.DeepEqual(got, []string{"gpt-5.4"}) {
		t.Fatalf("cold start availableModels() = %v", got)
	}

	// 还没到 TTL：不刷新。
	clock.advance(modelsCacheTTL - time.Second)
	triedBefore, _ := modelsProbe(t, r)
	if got := r.availableModels(); !reflect.DeepEqual(got, []string{"gpt-5.4"}) {
		t.Fatalf("within TTL availableModels() = %v, want the cached value", got)
	}
	assertNoQueryStarted(t, r, triedBefore, &hits, 1)

	// 过了 TTL：触发后台刷新，本次调用仍然先返回旧值。
	clock.advance(2 * time.Second)
	if got := r.availableModels(); !reflect.DeepEqual(got, []string{"gpt-5.4"}) {
		t.Fatalf("stale availableModels() = %v, want the stale cached value", got)
	}
	waitForModelsRefresh(t, r)
	if got := r.cachedModels(); !reflect.DeepEqual(got, []string{"gpt-5.5"}) {
		t.Fatalf("after refresh cachedModels() = %v, want the new value", got)
	}
}

func TestAvailableModelsKeepsCacheWhenQueryFails(t *testing.T) {
	var fail atomic.Bool
	r := newModelsRuntime(t, func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(modelsResponse("gpt-5.4", "gpt-5.5")))
	})

	clock := newTestClock()
	r.modelsNow = clock.now

	want := []string{"gpt-5.4", "gpt-5.5"}
	if got := r.availableModels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("cold start availableModels() = %v, want %v", got, want)
	}

	// 查询开始失败，缓存过期后仍必须回落到旧值，而不是变成空列表。
	fail.Store(true)
	clock.advance(modelsCacheTTL + time.Second)
	if got := r.availableModels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("failing query availableModels() = %v, want the cached %v", got, want)
	}
	waitForModelsRefresh(t, r)
	if got := r.availableModels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after a failed refresh availableModels() = %v, want the cached %v", got, want)
	}
}

// TestAvailableModelsDoesNotBlockOnInflightRefresh 锁住真正的回归点：
// 一次慢查询不能让后续请求跟着一起等。
func TestAvailableModelsDoesNotBlockOnInflightRefresh(t *testing.T) {
	release := make(chan struct{})
	arrived := make(chan struct{}, 1)
	r := newModelsRuntime(t, func(w http.ResponseWriter, _ *http.Request) {
		arrived <- struct{}{}
		<-release
		_, _ = w.Write([]byte(modelsResponse("gpt-5.4")))
	})
	defer close(release)

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.availableModels()
	}()

	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("the cold start query never reached the server")
	}

	start := time.Now()
	got := r.availableModels()
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("availableModels() waited %v on an in-flight refresh, want an immediate return", elapsed)
	}
	if len(got) != 0 {
		t.Fatalf("availableModels() = %v, want an empty list while the first query is still running", got)
	}

	release <- struct{}{}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the cold start call never returned")
	}
}

func waitForModelsRefresh(t *testing.T, r *Runtime) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.modelsMu.Lock()
		inflight := r.modelsInflight
		r.modelsMu.Unlock()
		if inflight == nil {
			return
		}
		select {
		case <-inflight:
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("the background models refresh did not finish in time")
}

func TestSortedModelsIsStableAndGrouped(t *testing.T) {
	// CPA 每次返回的次序都不同，这里用打乱后的真实列表。
	input := []string{
		"gpt-5.6-luna",
		"gpt-5.4",
		"gpt-5.6-sol",
		"gpt-5.5",
		"codex-auto-review",
		"gpt-5.4-mini",
		"gpt-5.6-terra",
	}
	want := []string{
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.5",
		"gpt-5.6-luna",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"codex-auto-review",
	}
	got := sortedModels(input)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedModels() = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(sortedModels(got), want) {
		t.Fatal("sortedModels is not idempotent")
	}
	if input[0] != "gpt-5.6-luna" {
		t.Fatal("sortedModels must not reorder the input slice")
	}
}

func TestSortedModelsComparesVersionsNumerically(t *testing.T) {
	got := sortedModels([]string{"gpt-5.10", "gpt-5.4", "gpt-5.2"})
	want := []string{"gpt-5.2", "gpt-5.4", "gpt-5.10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedModels() = %v, want %v", got, want)
	}
}
