package wake

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"quota-schedule-refresh/internal/config"
	"quota-schedule-refresh/internal/host"
)

type memoryHost struct {
	mu        sync.Mutex
	files     map[string][]byte
	meta      map[string]host.AuthFile
	executed  int
	saveErr   error
	rotateTok bool
	saves     int
}

func newMemoryHost(files ...host.AuthFile) *memoryHost {
	h := &memoryHost{
		files: make(map[string][]byte),
		meta:  make(map[string]host.AuthFile),
	}
	for _, file := range files {
		name := firstNonBlank(file.Name, file.ID)
		cloned := file
		cloned.Data = append([]byte(nil), file.Data...)
		h.meta[name] = cloned
		h.files[name] = append([]byte(nil), file.Data...)
	}
	return h
}

func (h *memoryHost) ModelExecute(_ context.Context, _ host.ModelExecuteRequest) (host.ModelExecuteResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.executed++
	if h.rotateTok {
		for name, data := range h.files {
			var doc map[string]any
			if json.Unmarshal(data, &doc) != nil {
				continue
			}
			doc["access_token"] = "rotated_" + strings.Repeat("z", 40)
			encoded, _ := json.Marshal(doc)
			h.files[name] = encoded
			meta := h.meta[name]
			meta.Data = encoded
			h.meta[name] = meta
		}
	}
	return host.ModelExecuteResponse{StatusCode: 200, Body: []byte(`{"choices":[{"message":{"content":"hello"}}]}`)}, nil
}

func (h *memoryHost) HTTPDo(_ context.Context, _ host.HTTPRequest) (host.HTTPResponse, error) {
	return host.HTTPResponse{}, errors.New("unused")
}

func (h *memoryHost) ListAuthFiles(_ context.Context) ([]host.AuthFile, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]host.AuthFile, 0, len(h.meta))
	for name, file := range h.meta {
		file.Data = append([]byte(nil), h.files[name]...)
		out = append(out, file)
	}
	return out, nil
}

func (h *memoryHost) GetAuthFile(_ context.Context, key string) (host.AuthFile, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for name, file := range h.meta {
		if name == key || file.ID == key || file.Name == key || file.AuthIndex == key {
			file.Data = append([]byte(nil), h.files[name]...)
			return file, nil
		}
	}
	return host.AuthFile{}, errors.New("not found")
}

func (h *memoryHost) GetRuntimeAuthFile(ctx context.Context, authIndex string) (host.AuthFile, error) {
	return h.GetAuthFile(ctx, authIndex)
}

func (h *memoryHost) SaveAuthFile(_ context.Context, name string, data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.saves++
	if h.saveErr != nil {
		return h.saveErr
	}
	h.files[name] = append([]byte(nil), data...)
	file, ok := h.meta[name]
	if !ok {
		file = host.AuthFile{Name: name, Provider: "codex"}
	}
	file.Data = append([]byte(nil), data...)
	h.meta[name] = file
	return nil
}

func (h *memoryHost) priority(name string) prioritySnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return snapshotFromJSON(h.files[name])
}

func (h *memoryHost) token(name string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var doc map[string]any
	_ = json.Unmarshal(h.files[name], &doc)
	text, _ := doc["access_token"].(string)
	return text
}

func authDoc(email string, priority *int) []byte {
	doc := map[string]any{
		"access_token":  "tok_" + strings.Repeat("x", 40),
		"refresh_token": "ref_" + strings.Repeat("y", 40),
		"type":          "codex",
		"email":         email,
	}
	if priority != nil {
		doc["priority"] = *priority
	}
	raw, _ := json.Marshal(doc)
	return raw
}

func ptrInt(v int) *int { return &v }

func testActivator(h host.Client) *Activator {
	cfg := config.Default()
	cfg.RetryCount = 0
	cfg.Prompt = "hello"
	return &Activator{Host: h, Config: cfg}
}

func TestNeedsPriorityBoost(t *testing.T) {
	if needsPriorityBoost(1000, 1000, 1) {
		t.Fatal("unique highest should not boost")
	}
	if !needsPriorityBoost(1000, 1000, 2) {
		t.Fatal("tied highest should boost")
	}
	if !needsPriorityBoost(1, 1000, 1) {
		t.Fatal("lower than max should boost")
	}
}

func TestBoostedPriorityFloor(t *testing.T) {
	if got := boostedPriority(0); got != 1000 {
		t.Fatalf("boostedPriority(0)=%d", got)
	}
	if got := boostedPriority(1000); got != 1001 {
		t.Fatalf("boostedPriority(1000)=%d", got)
	}
}

func TestUniqueHighestDoesNotWrite(t *testing.T) {
	h := newMemoryHost(host.AuthFile{
		ID: "a", Name: "a.json", Provider: "codex", Data: authDoc("a@x.com", ptrInt(1000)),
	})
	result := testActivator(h).Activate(context.Background(), "a", "a@x.com", "gpt-5.4", false)
	if !result.Success {
		t.Fatalf("result=%+v", result)
	}
	if h.priority("a.json").value != 1000 {
		t.Fatalf("priority changed: %+v", h.priority("a.json"))
	}
	if h.executed != 1 {
		t.Fatalf("executed=%d", h.executed)
	}
	if h.saves != 0 {
		t.Fatalf("unique highest wrote %d times, want 0", h.saves)
	}
}

func TestTiedPriorityBoostsThenRestores(t *testing.T) {
	h := newMemoryHost(
		host.AuthFile{ID: "a", Name: "a.json", Provider: "codex", Data: authDoc("a@x.com", ptrInt(1000))},
		host.AuthFile{ID: "b", Name: "b.json", Provider: "codex", Data: authDoc("b@x.com", ptrInt(1000))},
	)
	h.rotateTok = true
	result := testActivator(h).Activate(context.Background(), "a", "a@x.com", "gpt-5.4", false)
	if !result.Success {
		t.Fatalf("result=%+v", result)
	}
	got := h.priority("a.json")
	if !got.present || got.value != 1000 {
		t.Fatalf("restored priority=%+v, want 1000", got)
	}
	if peer := h.priority("b.json"); !peer.present || peer.value != 1000 {
		t.Fatalf("peer priority changed: %+v", peer)
	}
	if tok := h.token("a.json"); !strings.HasPrefix(tok, "rotated_") {
		t.Fatalf("restore overwrote rotated token: %s", tok)
	}
}

func TestMissingPriorityKeyIsDeletedOnRestore(t *testing.T) {
	h := newMemoryHost(
		host.AuthFile{ID: "a", Name: "a.json", Provider: "codex", Data: authDoc("a@x.com", nil)},
		host.AuthFile{ID: "b", Name: "b.json", Provider: "codex", Data: authDoc("b@x.com", nil)},
	)
	result := testActivator(h).Activate(context.Background(), "a", "a@x.com", "gpt-5.4", false)
	if !result.Success {
		t.Fatalf("result=%+v", result)
	}
	if h.priority("a.json").present {
		t.Fatal("priority key should be removed after restore")
	}
}

func TestBoostSaveFailureDoesNotExecute(t *testing.T) {
	h := newMemoryHost(
		host.AuthFile{ID: "a", Name: "a.json", Provider: "codex", Data: authDoc("a@x.com", ptrInt(1000))},
		host.AuthFile{ID: "b", Name: "b.json", Provider: "codex", Data: authDoc("b@x.com", ptrInt(1000))},
	)
	h.saveErr = errors.New("disk full")
	result := testActivator(h).Activate(context.Background(), "a", "a@x.com", "gpt-5.4", false)
	if result.Success || h.executed != 0 {
		t.Fatalf("executed=%d result=%+v", h.executed, result)
	}
	if !strings.Contains(result.LastError, "提升凭证优先级失败") {
		t.Fatalf("LastError=%q", result.LastError)
	}
}

func TestRestoreFailureKeepsSuccessAndNotes(t *testing.T) {
	h := newMemoryHost(
		host.AuthFile{ID: "a", Name: "a.json", Provider: "codex", Data: authDoc("a@x.com", ptrInt(1000))},
		host.AuthFile{ID: "b", Name: "b.json", Provider: "codex", Data: authDoc("b@x.com", ptrInt(1000))},
	)
	act := testActivator(h)
	result := act.withBoostedAuth(context.Background(), Result{AuthID: "a", Label: "a@x.com", Model: "gpt-5.4"}, func(ctx context.Context) Result {
		h.saveErr = errors.New("restore denied")
		out := act.executeOnce(ctx, "gpt-5.4", Result{AuthID: "a", Label: "a@x.com", Model: "gpt-5.4"})
		return out
	})
	if !result.Success {
		t.Fatalf("refresh itself should stay successful: %+v", result)
	}
	if !strings.Contains(result.Reply, restoreFailedMessage) {
		t.Fatalf("Reply=%q", result.Reply)
	}
	if !strings.Contains(result.Detail, restoreFailedMessage) {
		t.Fatalf("Detail=%q", result.Detail)
	}
}
