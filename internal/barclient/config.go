package barclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func DefaultConfig() Config {
	return Config{Boxes: []BoxConfig{}, RefreshSec: 30, OpenWith: "browser", Notify: false}
}

func ConfigPath(home, goos string) string {
	if goos == "windows" {
		return filepath.Join(home, "AppData", "Roaming", "boxdeck", "bar.json")
	}
	return filepath.Join(home, ".config", "boxdeck", "bar.json")
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return ConfigPath(home, runtime.GOOS), nil
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), os.ErrNotExist
		}
		return Config{}, err
	}
	config := DefaultConfig()
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	if err := ValidateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func ValidateConfig(config Config) error {
	if config.RefreshSec < 5 || config.RefreshSec > 3600 {
		return fmt.Errorf("refreshSec must be between 5 and 3600")
	}
	if config.OpenWith == "" {
		config.OpenWith = "browser"
	}
	if config.OpenWith != "browser" {
		return fmt.Errorf("openWith must be browser")
	}
	seen := map[string]bool{}
	for i, box := range config.Boxes {
		if strings.TrimSpace(box.Name) == "" {
			return fmt.Errorf("boxes[%d].name must be nonempty", i)
		}
		if box.URL == "" || (!strings.HasPrefix(box.URL, "http://") && !strings.HasPrefix(box.URL, "https://")) {
			return fmt.Errorf("boxes[%d].url must be an http(s) URL", i)
		}
		if seen[box.URL] {
			return fmt.Errorf("boxes[%d].url is duplicated", i)
		}
		seen[box.URL] = true
	}
	return nil
}

func WriteTemplate(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data := []byte("{\n  \"boxes\": [{\"name\": \"box\", \"url\": \"http://box:8100\", \"token\": \"\"}],\n  \"fleetToken\": \"\",\n  \"refreshSec\": 30,\n  \"openWith\": \"browser\",\n  \"notify\": false\n}\n")
	return os.WriteFile(path, data, 0600)
}
