package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"dps150-web/backend/internal/storage"
)

// SettingsKey is the settings-KV key the notification settings persist under
// (storage F-007 foundation model).
const SettingsKey = "notifications"

// SettingsStore is the persistence surface the notification settings need;
// *storage.Storage implements it.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

// EventSettings toggles notification delivery per event type. Field names
// and JSON tags follow the API contract v2 (F-015).
type EventSettings struct {
	ProtectionTrip  bool `json:"protectionTrip"`
	DeviceLink      bool `json:"deviceLink"`
	Output          bool `json:"output"`
	MeteringSession bool `json:"meteringSession"`
}

// Settings is the notification configuration of the API contract v2
// (F-015). The Telegram token and chat id are deliberately NOT part of it —
// they come only from the environment (see NewTelegramFromEnv).
type Settings struct {
	TelegramEnabled bool          `json:"telegramEnabled"`
	Events          EventSettings `json:"events"`
}

// DefaultSettings returns the settings used until the user saves their own:
// Telegram enabled with every event type on except output on/off, matching
// the contract example (output switching is routine, the rest is not).
func DefaultSettings() Settings {
	return Settings{
		TelegramEnabled: true,
		Events: EventSettings{
			ProtectionTrip:  true,
			DeviceLink:      true,
			Output:          false,
			MeteringSession: true,
		},
	}
}

// enabled reports whether notifications of the given kind are on.
func (e EventSettings) enabled(kind Kind) bool {
	switch kind {
	case KindProtectionTrip:
		return e.ProtectionTrip
	case KindDeviceLink:
		return e.DeviceLink
	case KindOutput:
		return e.Output
	case KindMeteringSession:
		return e.MeteringSession
	default:
		return false
	}
}

// LoadSettings returns the stored notification settings, or DefaultSettings
// when none were saved yet. It propagates storage.ErrUnavailable while the
// database is down.
func LoadSettings(ctx context.Context, store SettingsStore) (Settings, error) {
	raw, err := store.GetSetting(ctx, SettingsKey)
	if errors.Is(err, storage.ErrNotFound) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("load notification settings: %w", err)
	}
	var s Settings
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Settings{}, fmt.Errorf("load notification settings: corrupt value: %w", err)
	}
	return s, nil
}

// SaveSettings persists s, overwriting the previous settings.
func SaveSettings(ctx context.Context, store SettingsStore, s Settings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("save notification settings: %w", err)
	}
	if err := store.SetSetting(ctx, SettingsKey, string(raw)); err != nil {
		return fmt.Errorf("save notification settings: %w", err)
	}
	return nil
}
