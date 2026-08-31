package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingFileIsNotAnError(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nested", "settings.json"))
	data, found, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if found || data != nil {
		t.Fatalf("found=%v data=%q", found, data)
	}
}

func TestSaveThenLoad(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nested", "settings.json"))
	if err := s.Save([]byte(`{"daily_at":"09:00"}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, found, err := s.Load()
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}
	if string(data) != "{\"daily_at\":\"09:00\"}\n" {
		t.Fatalf("data = %q", data)
	}
	if err := s.Save([]byte(`{"daily_at":"10:00"}`)); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	data, _, err = s.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if string(data) != "{\"daily_at\":\"10:00\"}\n" {
		t.Fatalf("overwritten data = %q", data)
	}
}

func TestCorruptFileIsIgnored(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "settings.json"))
	if err := s.Save([]byte("not json")); err != nil {
		t.Fatalf("save: %v", err)
	}
	_, found, err := s.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if found {
		t.Fatal("损坏的设置文件不应被采用")
	}
}

func TestDefaultPathIncludesPluginID(t *testing.T) {
	path := DefaultPath()
	if filepath.Base(path) != fileName {
		t.Fatalf("path = %q", path)
	}
	if filepath.Base(filepath.Dir(path)) != pluginID {
		t.Fatalf("path = %q", path)
	}
}

func TestDefaultPathPrefersExistingPluginsDir(t *testing.T) {
	t.Setenv(envOverride, "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plugins"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	restore, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(restore)
	}()
	path := DefaultPath()
	want := filepath.Join("plugins", pluginID, fileName)
	if !strings.HasSuffix(path, want) {
		t.Fatalf("path = %q, want suffix %q", path, want)
	}
}

func TestDefaultPathHonoursEnvOverride(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv(envOverride, custom)
	if got := DefaultPath(); got != custom {
		t.Fatalf("path = %q, want %q", got, custom)
	}
}

func TestLoadFallsBackToLegacyLocation(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte(`{"daily_at":"06:30"}`), 0o600); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	s := &Store{path: filepath.Join(dir, "current.json"), legacy: []string{legacy}}
	data, found, err := s.Load()
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}
	if string(data) != `{"daily_at":"06:30"}` {
		t.Fatalf("data = %q", data)
	}
	if err := s.Save([]byte(`{"daily_at":"07:30"}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, _, err = s.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if string(data) != "{\"daily_at\":\"07:30\"}\n" {
		t.Fatalf("主路径应优先于历史位置，got %q", data)
	}
}
