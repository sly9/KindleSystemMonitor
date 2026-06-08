package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Kindle struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	Identity  string `json:"identity"`
	EIPS      string `json:"eips"`
	RemotePNG string `json:"remote_png"`
}

type Loop struct {
	IntervalSec float64 `json:"interval_sec"`
	Waveform    string  `json:"waveform"`
	FlushEvery  int     `json:"flush_every"`
	WelcomeSecs float64 `json:"welcome_secs"`
	NoFarewell  bool    `json:"no_farewell"`
}

type Messages struct {
	Welcome  []string `json:"welcome"`
	Farewell []string `json:"farewell"`
}

type Config struct {
	Kindle   Kindle   `json:"kindle"`
	Loop     Loop     `json:"loop"`
	Messages Messages `json:"messages"`

	SourcePath string `json:"-"`
}

func Defaults() Config {
	return Config{
		Kindle: Kindle{
			Port:      22,
			User:      "root",
			EIPS:      "/usr/sbin/eips",
			RemotePNG: "/tmp/dash.png",
		},
		Loop: Loop{
			IntervalSec: 10,
			Waveform:    "du",
			FlushEvery:  10,
			WelcomeSecs: 10,
		},
		Messages: Messages{
			Welcome: []string{
				"SYSTEM ONLINE",
				"",
				"欢迎回来",
				"高达驾驶员 Liuyi",
				"全系统已就绪",
			},
			Farewell: []string{
				"SYSTEM SHUTDOWN",
				"",
				"高达驾驶员 Liuyi",
				"本日作战结束",
				"后会有期",
			},
		},
	}
}

func DefaultPath() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(base, "kindle-dash", "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "kindle-dash", "config.json")
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	if path == "" {
		path = DefaultPath()
	}
	cfg.SourcePath = path

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.SourcePath = path
	return cfg, nil
}

func ExpandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
