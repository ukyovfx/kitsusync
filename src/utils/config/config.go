// Package config provides methods for accesing config file in TOML format
package config

import (
	"os"
	"strings"
	"unicode"

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

// UserMapEntry は Kitsu の名前と Discord ID の紐付け
type UserMapEntry struct {
	KitsuName string `toml:"kitsuName"` // Kitsu 上のフルネーム
	DiscordID string `toml:"discordID"` // Discord ユーザー ID
}

// CheckerEntry はタスクタイプ（工程）ごとのチェッカー
type CheckerEntry struct {
	TaskType  string `toml:"taskType"`  // タスクタイプ名（例: "Animation", "FX"）
	DiscordID string `toml:"discordID"` // チェッカーの Discord ID
}

// MentionConfig はメンション機能の全設定
type MentionConfig struct {
	ArtistStatuses  []string       `toml:"artistStatuses"`  // アーティストにメンションするステータス
	CheckerStatuses []string       `toml:"checkerStatuses"` // チェッカーにメンションするステータス
	HereStatuses    []string       `toml:"hereStatuses"`    // @here を飛ばす緊急ステータス
	UserMap         []UserMapEntry `toml:"userMap"`         // Kitsu名 → Discord ID
	Checkers        []CheckerEntry `toml:"checkers"`        // タスクタイプ → チェッカー Discord ID
}

// NotificationConfig は通知機能の設定
type NotificationConfig struct {
	NotifyOnAssign bool `toml:"notifyOnAssign"` // true なら TODO（新規アサイン）ステータスの変化も通知する
}

type Config struct {
	TplPreset             string
	IgnoreMessagesDaysOld int
	SilentUpdateDB        bool
	Threads               int
	Debug                 bool
	Log                   bool
	Kitsu                 struct {
		Hostname        string
		Email           string
		Password        string
		SkipComments    bool
		RequestInterval int
	}
	Discord struct {
		EmbedsPerRequests int
		RequestsPerMinute int
		WebhookURL        string
		UseThreads        bool              `toml:"useThreads"`
		BotToken          string            `toml:"botToken"` // Discord Bot トークン（${DISCORD_BOT_TOKEN}）
		GuildID           string            `toml:"guildID"`  // Discord サーバー ID
		Productions       []Productions     `toml:"productions,omitempty"`
		TaskTypeWebhooks  []TaskTypeWebhook `toml:"taskTypeWebhooks,omitempty"`
	}
	Mention      MentionConfig      `toml:"mention"`
	Notification NotificationConfig `toml:"notification"`
	GoogleDrive  struct {
		URL string `toml:"url"`
	} `toml:"googleDrive"`
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
	raw, err := os.ReadFile(path)
	if err != nil {
		slog.Fatal(err)
	}

	expanded := expandEnvBraces(string(raw))

	var config Config
	if err := toml.NewDecoder(strings.NewReader(expanded)).Decode(&config); err != nil {
		slog.Fatal(err)
	}
	return config
}

// Validate は設定内容を検査して問題のある項目を文字列スライスで返す。
// 致命的な必須フィールド未設定から、プレースホルダが残っているだけの警告レベルまで含む。
// 呼び出し側は返り値をループして slog.Warn / slog.Error 等で出力する。
func (c *Config) Validate() []string {
	var issues []string

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

	// --- テンプレートプレースホルダ検出 ---
	for _, u := range c.Mention.UserMap {
		if looksLikePlaceholder(u.KitsuName) {
			issues = append(issues, "[WARN] mention.userMap: kitsuName looks like a placeholder — update it: "+u.KitsuName)
		}
		if looksLikePlaceholder(u.DiscordID) {
			issues = append(issues, "[WARN] mention.userMap: discordID looks like a placeholder for kitsuName="+u.KitsuName+" — update it: "+u.DiscordID)
		}
	}
	for _, ch := range c.Mention.Checkers {
		if looksLikePlaceholder(ch.DiscordID) {
			issues = append(issues, "[WARN] mention.checkers: discordID looks like a placeholder for taskType="+ch.TaskType+" — update it: "+ch.DiscordID)
		}
	}

	return issues
}

// looksLikePlaceholder は文字列がプレースホルダ（未設定値）らしいかどうかを返す。
// 判定基準:
//   - 日本語（CJK/ひらがな/カタカナ等）を含む → テンプレートの日本語説明文
//   - "000000000000000000" や "222222222222222222" など明らかなダミー Discord ID
func looksLikePlaceholder(s string) bool {
	if s == "" {
		return false // 空は別途必須チェックで拾う
	}
	for _, r := range s {
		// ひらがな / カタカナ / CJK統合漢字 / CJK記号 など
		if unicode.In(r,
			unicode.Hiragana,
			unicode.Katakana,
			unicode.Han,
		) {
			return true
		}
	}
	// よく使われるダミー Discord ID パターン
	knownDummies := []string{
		"222222222222222222",
		"000000000000000000",
		"111111111111111111",
		"999999999999999999",
	}
	for _, d := range knownDummies {
		if s == d {
			return true
		}
	}
	return false
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
