package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/iamtew/ting/internal/ai"
)

const (
	settingAISystemPrompt  = "ai_system_prompt"
	settingAIEnabled       = "ai_enabled"
	settingAISampling      = "ai_sampling"
	settingAIModels        = "ai_models"
	settingAIMemoryEnabled = "ai_memory_enabled"
	settingAIMemoryWindow  = "ai_memory_window"

	// DefaultAISystemPrompt is used until a saved prompt exists.
	DefaultAISystemPrompt = "You are ting, an IRC bot. Keep replies to one or two short sentences. Be plain and useful. Do not dump long lists unless asked."

	DefaultAIMemoryWindow = 8
	MinAIMemoryWindow     = 1
	MaxAIMemoryWindow     = 12

	// MaxAISystemPromptRunes is the admin textarea cap.
	MaxAISystemPromptRunes = 8000
)

func (d *DB) getSetting(key string) (value string, present bool, err error) {
	err = d.sql.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, true, nil
}

func (d *DB) setSetting(key, value string) error {
	_, err := d.sql.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

// AISystemPrompt returns the saved prompt, or the built-in default when missing or blank.
func (d *DB) AISystemPrompt() (string, error) {
	v, present, err := d.getSetting(settingAISystemPrompt)
	if err != nil {
		return "", err
	}
	if !present || strings.TrimSpace(v) == "" {
		return DefaultAISystemPrompt, nil
	}
	return strings.TrimSpace(v), nil
}

// SetAISystemPrompt stores a prompt. Blank falls back to the default on read.
func (d *DB) SetAISystemPrompt(prompt string) error {
	return d.setSetting(settingAISystemPrompt, strings.TrimSpace(prompt))
}

// AIEnabled is whether mention and query chat is on. Missing key → true.
func (d *DB) AIEnabled() (bool, error) {
	v, present, err := d.getSetting(settingAIEnabled)
	if err != nil {
		return false, err
	}
	if !present {
		return true, nil
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "off", "no":
		return false, nil
	case "1", "true", "on", "yes":
		return true, nil
	default:
		return true, nil
	}
}

// SetAIEnabled writes the chat on/off flag.
func (d *DB) SetAIEnabled(on bool) error {
	val := "0"
	if on {
		val = "1"
	}
	return d.setSetting(settingAIEnabled, val)
}

// AIMemoryEnabled is whether channel replies include recent lines. Missing → false.
func (d *DB) AIMemoryEnabled() (bool, error) {
	v, present, err := d.getSetting(settingAIMemoryEnabled)
	if err != nil {
		return false, err
	}
	if !present {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// SetAIMemoryEnabled writes the memory on/off flag.
func (d *DB) SetAIMemoryEnabled(on bool) error {
	val := "0"
	if on {
		val = "1"
	}
	return d.setSetting(settingAIMemoryEnabled, val)
}

// NormalizeAIMemoryWindow returns n if it is 1–12.
func NormalizeAIMemoryWindow(n int) (int, error) {
	if n < MinAIMemoryWindow || n > MaxAIMemoryWindow {
		return 0, fmt.Errorf("memory window must be %d–%d", MinAIMemoryWindow, MaxAIMemoryWindow)
	}
	return n, nil
}

// AIMemoryWindow is how many prior channel lines to include. Missing or junk → 8.
func (d *DB) AIMemoryWindow() (int, error) {
	v, present, err := d.getSetting(settingAIMemoryWindow)
	if err != nil {
		return 0, err
	}
	if !present || strings.TrimSpace(v) == "" {
		return DefaultAIMemoryWindow, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return DefaultAIMemoryWindow, nil
	}
	out, err := NormalizeAIMemoryWindow(n)
	if err != nil {
		return DefaultAIMemoryWindow, nil
	}
	return out, nil
}

// SetAIMemoryWindow writes a window in 1–12.
func (d *DB) SetAIMemoryWindow(n int) error {
	out, err := NormalizeAIMemoryWindow(n)
	if err != nil {
		return err
	}
	return d.setSetting(settingAIMemoryWindow, fmt.Sprintf("%d", out))
}

// LoadAISampling reads admin sampling sliders, or built-in defaults when unset.
func (d *DB) LoadAISampling() (ai.Sampling, error) {
	raw, present, err := d.getSetting(settingAISampling)
	if err != nil {
		return ai.Sampling{}, err
	}
	if !present || strings.TrimSpace(raw) == "" {
		return ai.DefaultSampling(), nil
	}
	out, err := ai.ParseSamplingJSON([]byte(raw))
	if err != nil {
		return ai.Sampling{}, fmt.Errorf("ai sampling settings: %w", err)
	}
	return out, nil
}

// SaveAISampling clamps and writes the admin sampling sliders.
func (d *DB) SaveAISampling(in ai.Sampling) (ai.Sampling, error) {
	out := ai.NormalizeSampling(in)
	b, err := json.Marshal(out)
	if err != nil {
		return ai.Sampling{}, err
	}
	if err := d.setSetting(settingAISampling, string(b)); err != nil {
		return ai.Sampling{}, err
	}
	return out, nil
}

// LoadAIModels reads the admin catalog. bootstrap is used only when the key is missing.
func (d *DB) LoadAIModels(bootstrap string) (ai.ModelCatalog, error) {
	raw, present, err := d.getSetting(settingAIModels)
	if err != nil {
		return ai.ModelCatalog{}, err
	}
	if !present || strings.TrimSpace(raw) == "" {
		return ai.DefaultCatalog(bootstrap), nil
	}
	var in ai.ModelCatalog
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return ai.ModelCatalog{}, fmt.Errorf("ai model settings: %w", err)
	}
	out, err := ai.NormalizeCatalog(in)
	if err != nil {
		return ai.ModelCatalog{}, fmt.Errorf("ai model settings: %w", err)
	}
	return out, nil
}

// SaveAIModels clamps and writes the admin catalog.
func (d *DB) SaveAIModels(in ai.ModelCatalog) (ai.ModelCatalog, error) {
	out, err := ai.NormalizeCatalog(in)
	if err != nil {
		return ai.ModelCatalog{}, err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ai.ModelCatalog{}, err
	}
	if err := d.setSetting(settingAIModels, string(b)); err != nil {
		return ai.ModelCatalog{}, err
	}
	return out, nil
}
