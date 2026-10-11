package config

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Config — настройки из ~/.config/gw/config (KEY=VALUE) и окружения; окружение важнее.
type Config struct {
	GitHubToken string

	// демон
	Port         string
	DBPath       string
	PollInterval time.Duration
	TelegramBot  string // токен бота от @BotFather
	TelegramChat string // куда слать (твой chat id)
	NotifyKinds  string // "ci_failed,mention" | "all" | "" — по умолчанию
	NotifyRepos  string // "me/app,me/api" | "" — все

	// TUI/CLI → демон
	DaemonURL string
}

// Path — файл с настройками. Его же дополняет `gw telegram`.
func Path() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "gw", "config")
	}
	home, err := os.UserHomeDir() // ~/.config и на macOS — привычнее, чем Application Support
	if err != nil {
		return "gw.config"
	}
	return filepath.Join(home, ".config", "gw", "config")
}

func Load() Config {
	file := readFile(Path())
	get := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v := file[key]; v != "" {
			return v
		}
		return fallback
	}
	poll, err := time.ParseDuration(get("POLL_INTERVAL", "1m"))
	if err != nil || poll < 10*time.Second {
		poll = time.Minute
	}
	token := get("GITHUB_TOKEN", "")
	if token == "" {
		token = ghToken()
	}
	return Config{
		GitHubToken:  token,
		Port:         get("PORT", "7777"),
		DBPath:       get("GW_DB", defaultDBPath()),
		PollInterval: poll,
		TelegramBot:  get("TELEGRAM_BOT_TOKEN", ""),
		TelegramChat: get("TELEGRAM_CHAT_ID", ""),
		NotifyKinds:  get("NOTIFY_KINDS", ""),
		NotifyRepos:  get("NOTIFY_REPOS", ""),
		DaemonURL:    get("GW_DAEMON_URL", "http://localhost:"+get("PORT", "7777")),
	}
}

// Save дописывает/заменяет ключи в файле настроек, остальные ключи сохраняет.
func Save(values map[string]string) error {
	path := Path()
	current := readFile(path)
	maps.Copy(current, values)
	keys := make([]string, 0, len(current))
	for k := range current {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# настройки gw — KEY=VALUE, переменные окружения важнее\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, current[k])
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o600) // там токены — только для владельца
}

func readFile(path string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return out
}

func defaultDBPath() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "gw", "gw.db")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "gw.db"
	}
	return filepath.Join(home, ".local", "share", "gw", "gw.db")
}

// ghToken — токен из `gh auth login`, чтобы не логиниться дважды.
func ghToken() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
