package notify

import (
	"context"
	"testing"
)

func TestDefaultSettings(t *testing.T) {
	got := DefaultSettings()
	want := Settings{
		TelegramEnabled: true,
		Events: EventSettings{
			ProtectionTrip:  true,
			DeviceLink:      true,
			Output:          false,
			MeteringSession: true,
		},
	}
	if got != want {
		t.Errorf("DefaultSettings() = %+v, want %+v", got, want)
	}
}

func TestSettingsLoadDefaultsWhenUnset(t *testing.T) {
	store := newMemStore()
	got, err := LoadSettings(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != DefaultSettings() {
		t.Errorf("LoadSettings() = %+v, want defaults", got)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	store := newMemStore()
	want := Settings{
		TelegramEnabled: false,
		Events: EventSettings{
			ProtectionTrip:  true,
			DeviceLink:      false,
			Output:          true,
			MeteringSession: false,
		},
	}
	if err := SaveSettings(context.Background(), store, want); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := LoadSettings(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != want {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}

func TestSettingsLoadCorruptValue(t *testing.T) {
	store := newMemStore()
	if err := store.SetSetting(context.Background(), SettingsKey, "{broken"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(context.Background(), store); err == nil {
		t.Error("LoadSettings succeeded on a corrupt value")
	}
}
