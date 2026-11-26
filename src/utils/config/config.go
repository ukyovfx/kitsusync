// Package config provides methods for accesing config file in TOML format
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/gookit/slog"
	"github.com/naoina/toml"
)

type Productions struct {
	Production string
	WebhookURL string
}

// TaskTypeWebhook はタスクタイプ（工程）ごとの送信先 Webhook
type TaskTypeWebhook struct {
	TaskType   string `toml:"taskType"`
	WebhookURL string `toml:"webhookURL"`
}

// NotificationConfig は通知機能の設定
type NotificationConfig struct {
	DeliveryPayloadRetentionDays int `toml:"deliveryPayloadRetentionDays"`
}

type Config struct {
	TplPreset             string
	IgnoreMessagesDaysOld int
	SilentUpdateDB        bool
	Threads               int
	Debug                 bool
	Log                   bool
	Kitsu                 struct {
		Hostname            string
		Email               string
		Password            string
		SkipComments        bool
		RequestInterval     int
		PollIntervalSeconds int `toml:"pollIntervalSeconds"`
		// MaxResponseMB は Kitsu 応答本文の上限（MiB）。0 なら既定（64 MiB）。
		MaxResponseMB int `toml:"maxResponseMB"`
	}
	Discord struct {
		EmbedsPerRequests int
		RequestsPerMinute int
		WebhookURL        string
		BotToken          string            `toml:"botToken"` // Discord Bot トークン（${DISCORD_BOT_TOKEN}）
		GuildID           string            `toml:"guildID"`  // Discord サーバー ID
		Productions       []Productions     `toml:"productions,omitempty"`
		TaskTypeWebhooks  []TaskTypeWebhook `toml:"taskTypeWebhooks,omitempty"`
	}
	Notification NotificationConfig `toml:"notification"`
	GoogleDrive  struct {
		URL string `toml:"url"`
	} `toml:"googleDrive"`
}

const (
	minPollIntervalSeconds              = 5
	maxPollIntervalSeconds              = 60
	defaultPollIntervalSeconds          = 10
	defaultDeliveryPayloadRetentionDays = 30
	maxDeliveryPayloadRetentionDays     = 3650
)

// PollInterval returns the configured polling cadence. The explicit seconds
// setting takes precedence; older configurations continue to interpret
// requestInterval as minutes so requestInterval=1 never becomes one-second
// polling.
func (c Config) PollInterval() time.Duration {
	seconds := c.PollIntervalSeconds()
	return time.Duration(seconds) * time.Second
}

func (c Config) PollIntervalSeconds() int {
	if c.Kitsu.PollIntervalSeconds > 0 {
		return c.Kitsu.PollIntervalSeconds
	}
	if c.Kitsu.RequestInterval > 0 {
		return c.Kitsu.RequestInterval * 60
	}
	return defaultPollIntervalSeconds
}

// Read loads conf.toml, expanding ${VAR} placeholders from environment variables.
//
// This lets you keep secrets (Kitsu password, Discord webhook URL) out of the
// committed conf.toml — write `password = "${KITSU_PASSWORD}"` in the file,
// and the value is filled in from the environment at load time.
//
// Only the ${VAR} form is expanded — bare `$VAR` is left alone so that
// passwords containing literal `$` are preserved.
func Read() Config {
	path := "conf.toml"
	if os.Getenv("TEST") == "true" {
		path = os.Getenv("CONF_PATH")
	}
	config, err := ReadFromPath(path)
	if err != nil {
		slog.Fatal(err)
	}
	return config
}

// ReadFromPath loads a config file without exiting the process. It provides a
// typed directory diagnostic so a bad bind mount is distinguishable from a
// malformed or missing configuration file.
func ReadFromPath(path string) (Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, fmt.Errorf("read conf.toml: %w", err)
	}
	if info.IsDir() {
		return Config{}, fmt.Errorf("read conf.toml: configured path is a directory (%s)", filepath.Base(path))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read conf.toml: %w", err)
	}

	expanded := expandEnvBraces(string(raw))

	config := Config{Notification: NotificationConfig{
		DeliveryPayloadRetentionDays: defaultDeliveryPayloadRetentionDays,
	}}
	// 未知のキーは無視する。構造体から削除した設定項目が残る conf.toml でも起動できるようにするため。
	tomlCfg := toml.DefaultConfig
	tomlCfg.MissingField = func(reflect.Type, string) error { return nil }
	if err := tomlCfg.NewDecoder(strings.NewReader(expanded)).Decode(&config); err != nil {
		return Config{}, fmt.Errorf("parse conf.toml: %w", err)
	}
	return config, nil
}

// Validate は設定内容を検査して問題のある項目を文字列スライスで返す。
// 致命的な必須フィールド未設定から、プレースホルダが残っているだけの警告レベルまで含む。
// 呼び出し側は返り値をループして slog.Warn / slog.Error 等で出力する。
func (c *Config) Validate() []string {
	var issues []string

	// NOTE: These two checks are load-bearing, not dead validation. When
	// conf.toml cannot be read, Read() logs the failure but the process
	// continues with zero-value defaults, so these [FATAL] issues are the
	// mechanism that makes the app fail fast instead of starting with an
	// empty configuration. Removing them made the staging runtime E2E hang:
	// the app started with no config and never exited (PR 388 CI, 2026-11-26).
	if c.Discord.EmbedsPerRequests <= 0 {
		issues = append(issues, "[FATAL] discord.embedsPerRequests must be greater than zero")
	}
	if c.Discord.RequestsPerMinute <= 0 {
		issues = append(issues, "[FATAL] discord.requestsPerMinute must be greater than zero")
	}

	if c.Kitsu.PollIntervalSeconds != 0 && (c.Kitsu.PollIntervalSeconds < minPollIntervalSeconds || c.Kitsu.PollIntervalSeconds > maxPollIntervalSeconds) {
		issues = append(issues, fmt.Sprintf("[FATAL] kitsu.pollIntervalSeconds must be between %d and %d", minPollIntervalSeconds, maxPollIntervalSeconds))
	}
	if c.Kitsu.MaxResponseMB < 0 {
		issues = append(issues, "[FATAL] kitsu.maxResponseMB must be zero (default) or a positive number of MiB")
	}
	if c.Notification.DeliveryPayloadRetentionDays < 0 || c.Notification.DeliveryPayloadRetentionDays > maxDeliveryPayloadRetentionDays {
		issues = append(issues, fmt.Sprintf("[FATAL] notification.deliveryPayloadRetentionDays must be between 0 and %d", maxDeliveryPayloadRetentionDays))
	}

	// --- 必須フィールド ---
	if strings.TrimSpace(c.Kitsu.Hostname) == "" {
		issues = append(issues, "[SETUP] kitsu.hostname is empty — configure it in the Web UI or conf.toml")
	}
	if strings.TrimSpace(c.Kitsu.Email) == "" {
		issues = append(issues, "[SETUP] kitsu.email is empty — runtime polling remains paused")
	}
	if strings.TrimSpace(c.Kitsu.Password) == "" {
		issues = append(issues, "[SETUP] kitsu.password is empty — runtime polling remains paused")
	}
	if strings.TrimSpace(c.Discord.WebhookURL) == "" {
		issues = append(issues, "[WARN] discord.webhookURL is empty — project routing can be configured in the Web UI")
	}

	return issues
}

// expandEnvBraces replaces ${VAR} occurrences with the value of the environment
// variable VAR. Unset variables expand to the empty string. The bare $VAR form
// is intentionally NOT expanded — this avoids mangling values that legitimately
// contain $ (e.g. some auto-generated passwords).
func expandEnvBraces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				b.WriteString(s[i:])
				break
			}
			name := s[i+2 : i+2+end]
			b.WriteString(os.Getenv(name))
			i += 2 + end + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
