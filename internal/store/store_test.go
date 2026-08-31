package store

import (
	"path/filepath"
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
	if filepath.Base(path) != "settings.json" {
		t.Fatalf("path = %q", path)
	}
	if filepath.Base(filepath.Dir(path)) != pluginID {
		t.Fatalf("path = %q", path)
	}
}
