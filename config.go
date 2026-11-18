package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const configFileName = "config.json"

var errNoConfig = errors.New("chưa có cấu hình. Chạy `glops setup` để lưu endpoint, username và token")

type Config struct {
	Endpoint           string `json:"endpoint"`
	Username           string `json:"username"`
	Token              string `json:"token"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
}

func (a *App) configFile() (string, error) {
	dir := a.ConfigDir
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("không tìm thấy thư mục cấu hình: %w", err)
		}
		dir = filepath.Join(base, "glops")
	}
	return filepath.Join(dir, configFileName), nil
}

func (a *App) loadConfig() (Config, error) {
	path, err := a.configFile()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, errNoConfig
		}
		return Config{}, fmt.Errorf("không đọc được %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("file cấu hình %s không đọc được: %w", path, err)
	}
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.Token = strings.TrimSpace(cfg.Token)
	if cfg.Endpoint == "" || cfg.Token == "" {
		return Config{}, fmt.Errorf("cấu hình %s thiếu endpoint hoặc token. Chạy `glops setup` lại", path)
	}
	normalized, err := normalizeEndpoint(cfg.Endpoint)
	if err != nil {
		return Config{}, fmt.Errorf("endpoint trong cấu hình không hợp lệ: %w", err)
	}
	cfg.Endpoint = normalized
	return cfg, nil
}

func (a *App) loadOptional() (*Config, error) {
	cfg, err := a.loadConfig()
	if errors.Is(err, errNoConfig) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (a *App) saveConfig(cfg Config) (string, error) {
	path, err := a.configFile()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := writePrivate(path, data); err != nil {
		return "", err
	}
	return path, nil
}

func writePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	success = true
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return nil
}

func normalizeEndpoint(raw string) (string, error) {
	raw = trimWrapping(raw)
	if raw == "" {
		return "", errors.New("endpoint không được để trống")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("endpoint không hợp lệ")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("endpoint chỉ hỗ trợ http hoặc https")
	}
	if u.User != nil {
		return "", errors.New("endpoint không được kèm user hoặc mật khẩu. Token lưu riêng khi setup")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("endpoint không được kèm query hoặc fragment")
	}
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	endpoint := u.Scheme + "://" + host
	if p := strings.Trim(u.Path, "/"); p != "" {
		if strings.ContainsAny(p, " ?#") {
			return "", errors.New("đường dẫn endpoint không hợp lệ")
		}
		endpoint += "/" + p
	}
	return endpoint, nil
}

func maskToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 4 {
		return "****"
	}
	return "****" + token[len(token)-4:]
}

func trimWrapping(s string) string {
	s = strings.TrimSpace(s)
	for len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') || (first == '<' && last == '>') {
			s = strings.TrimSpace(s[1 : len(s)-1])
			continue
		}
		break
	}
	return s
}
