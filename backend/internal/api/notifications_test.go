package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"dps150-web/backend/internal/notify"
	"dps150-web/backend/internal/storage"
)

// openSettingsStorage opens a ready SQLite storage in a temp dir.
func openSettingsStorage(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)
	deadline := time.Now().Add(5 * time.Second)
	for !s.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("storage did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s
}

func TestGetNotificationSettingsDefaults(t *testing.T) {
	WireNotifications(openSettingsStorage(t), true)
	w := doRequest(t, &fakeHub{}, http.MethodGet, "/api/v1/settings/notifications", "")

	if w.Code != http.StatusOK {
		t.Fatalf("GET settings = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	// No "configured" key while the Telegram env is set.
	assertJSON(t, w.Body.String(), `{
		"telegramEnabled": true,
		"events": {
			"protectionTrip": true,
			"deviceLink": true,
			"output": false,
			"meteringSession": true
		}
	}`)
}

func TestGetNotificationSettingsUnconfigured(t *testing.T) {
	WireNotifications(openSettingsStorage(t), false)
	w := doRequest(t, &fakeHub{}, http.MethodGet, "/api/v1/settings/notifications", "")

	if w.Code != http.StatusOK {
		t.Fatalf("GET settings = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	assertJSON(t, w.Body.String(), `{
		"telegramEnabled": true,
		"events": {
			"protectionTrip": true,
			"deviceLink": true,
			"output": false,
			"meteringSession": true
		},
		"configured": false
	}`)
}

func TestPutNotificationSettingsRoundTrip(t *testing.T) {
	store := openSettingsStorage(t)
	WireNotifications(store, true)
	w := doRequest(t, &fakeHub{}, http.MethodPut, "/api/v1/settings/notifications", `{
		"telegramEnabled": false,
		"events": {
			"protectionTrip": true,
			"deviceLink": false,
			"output": true,
			"meteringSession": false
		}
	}`)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT settings = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	want := `{
		"telegramEnabled": false,
		"events": {
			"protectionTrip": true,
			"deviceLink": false,
			"output": true,
			"meteringSession": false
		}
	}`
	assertJSON(t, w.Body.String(), want)

	// GET reads back the same document.
	w = doRequest(t, &fakeHub{}, http.MethodGet, "/api/v1/settings/notifications", "")
	assertJSON(t, w.Body.String(), want)

	// The settings actually persisted into the KV store.
	saved, err := notify.LoadSettings(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if saved.TelegramEnabled || !saved.Events.Output || saved.Events.DeviceLink {
		t.Errorf("persisted settings = %+v, do not match the PUT body", saved)
	}
}

func TestPutNotificationSettingsPartial(t *testing.T) {
	WireNotifications(openSettingsStorage(t), true)
	w := doRequest(t, &fakeHub{}, http.MethodPut, "/api/v1/settings/notifications",
		`{"events": {"output": true}}`)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT settings = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	// Only output changed; the rest kept the defaults.
	assertJSON(t, w.Body.String(), `{
		"telegramEnabled": true,
		"events": {
			"protectionTrip": true,
			"deviceLink": true,
			"output": true,
			"meteringSession": true
		}
	}`)
}

func TestPutNotificationSettingsBadBody(t *testing.T) {
	WireNotifications(openSettingsStorage(t), true)
	for _, body := range []string{`not json`, `{"telegramEnabled": "yes"}`, `{"events": 5}`} {
		w := doRequest(t, &fakeHub{}, http.MethodPut, "/api/v1/settings/notifications", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want %d", body, w.Code, http.StatusBadRequest)
			continue
		}
		if code := errorCode(t, w.Body.String()); code != "bad_request" {
			t.Errorf("error code = %q, want bad_request", code)
		}
	}
}

func TestNotificationSettingsStorageDisabled(t *testing.T) {
	WireNotifications(nil, true)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		body := ""
		if method == http.MethodPut {
			body = `{"telegramEnabled": true}`
		}
		w := doRequest(t, &fakeHub{}, method, "/api/v1/settings/notifications", body)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s settings = %d, want %d", method, w.Code, http.StatusServiceUnavailable)
			continue
		}
		if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
			t.Errorf("error code = %q, want storage_unavailable", code)
		}
	}
}

func TestNotificationSettingsStorageDown(t *testing.T) {
	// A postgres storage that can never connect: data methods return
	// ErrUnavailable, the endpoint answers 503 storage_unavailable.
	s, err := storage.Open(storage.Config{
		Driver:     storage.DriverPostgres,
		DSN:        "postgres://127.0.0.1:1/nope",
		BackoffMin: time.Hour, // no reconnect churn during the test
	})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(s.Close)
	WireNotifications(s, true)

	w := doRequest(t, &fakeHub{}, http.MethodGet, "/api/v1/settings/notifications", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET settings = %d, want %d: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	if code := errorCode(t, w.Body.String()); code != "storage_unavailable" {
		t.Errorf("error code = %q, want storage_unavailable", code)
	}
}
