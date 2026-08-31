// Package store 负责插件私有设置文件的读写。
// CPA 的 config_fields 协议无法表达默认值，宿主表单会把未填写的项渲染成空值或关闭，
// 因此设置由插件页面自行维护，config.yaml 仅作为首次启动的基线。
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const pluginID = "quota-schedule-refresh"

// Store 读写单个 JSON 文件，路径缺省时按宿主约定落在用户配置目录下。
type Store struct {
	mu   sync.Mutex
	path string
}

func New(path string) *Store {
	if path == "" {
		path = DefaultPath()
	}
	return &Store{path: path}
}

// DefaultPath 与同类 CPA 插件保持一致：<用户配置目录>/CLIProxyAPI/<插件 ID>/settings.json。
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "CLIProxyAPI", pluginID, "settings.json")
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load 返回文件内容。文件不存在时 found 为 false，且不视为错误。
func (s *Store) Load() (data []byte, found bool, err error) {
	if s == nil || s.path == "" {
		return nil, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, false, nil
	}
	return raw, true, nil
}

// Save 原子写入：先落临时文件再改名，避免宿主进程读到半个文件。
func (s *Store) Save(data []byte) error {
	if s == nil || s.path == "" {
		return errors.New("设置文件路径为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	temp, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if _, err := temp.Write(append(data, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tempPath, s.path)
}
