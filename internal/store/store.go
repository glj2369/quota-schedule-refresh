// Package store 负责插件私有设置文件的读写。
// CPA 的 config_fields 协议无法表达默认值，宿主表单会把未填写的项渲染成空值或关闭，
// 因此设置由插件页面自行维护，config.yaml 仅作为首次启动的基线。
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	pluginID    = "quota-schedule-refresh"
	fileName    = "settings.json"
	envOverride = "QUOTA_SCHEDULE_REFRESH_SETTINGS"
)

// Store 读写单个 JSON 文件。写入走主路径，读取时会回退到历史位置，便于换位置后平滑接管。
type Store struct {
	mu     sync.Mutex
	path   string
	legacy []string
}

func New(path string) *Store {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultPath()
	}
	s := &Store{path: path}
	if fallback := userConfigPath(); fallback != "" && fallback != path {
		s.legacy = append(s.legacy, fallback)
	}
	return s
}

// DefaultPath 选择一个能跨容器重建存活的位置。
// 官方镜像只挂载 plugins、config.yaml、auths 和 logs，用户配置目录属于容器可写层，
// 因此优先写到 plugins 目录下，裸机部署再回落到用户配置目录。
func DefaultPath() string {
	if custom := strings.TrimSpace(os.Getenv(envOverride)); custom != "" {
		return custom
	}
	for _, dir := range pluginDirs() {
		target := filepath.Join(dir, pluginID)
		if err := os.MkdirAll(target, 0o700); err != nil {
			continue
		}
		return filepath.Join(target, fileName)
	}
	if fallback := userConfigPath(); fallback != "" {
		return fallback
	}
	return filepath.Join(".", pluginID, fileName)
}

// pluginDirs 只返回已经存在的 plugins 目录，避免在无关工作目录里凭空造目录。
func pluginDirs() []string {
	roots := make([]string, 0, 2)
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		roots = append(roots, cwd)
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		roots = append(roots, filepath.Dir(exe))
	}
	dirs := make([]string, 0, len(roots))
	seen := make(map[string]bool, len(roots))
	for _, root := range roots {
		dir := filepath.Join(root, "plugins")
		if seen[dir] {
			continue
		}
		seen[dir] = true
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

func userConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "CLIProxyAPI", pluginID, fileName)
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
	var firstErr error
	for _, path := range append([]string{s.path}, s.legacy...) {
		raw, err := readSettings(path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if raw != nil {
			return raw, true, nil
		}
	}
	return nil, false, firstErr
}

func readSettings(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, nil
	}
	return raw, nil
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
