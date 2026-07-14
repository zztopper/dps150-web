package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTelegramSend(t *testing.T) {
	api, srv := newBotAPI(t)
	tg := NewTelegram("SECRET", "1234", WithBaseURL(srv.URL))

	if !tg.Configured() {
		t.Fatal("Configured() = false with both credentials set")
	}
	if err := tg.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := api.messages()
	if len(got) != 1 {
		t.Fatalf("bot API calls = %d, want 1", len(got))
	}
	if got[0].path != "/botSECRET/sendMessage" {
		t.Errorf("path = %q, want /botSECRET/sendMessage", got[0].path)
	}
	if got[0].chatID != "1234" || got[0].text != "hello" {
		t.Errorf("message = %+v, want chat 1234 / text hello", got[0])
	}
}

func TestTelegramSendHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok": false, "description": "Unauthorized"}`))
	}))
	t.Cleanup(srv.Close)
	tg := NewTelegram("BAD", "1", WithBaseURL(srv.URL))

	if err := tg.Send(context.Background(), "x"); err == nil {
		t.Fatal("Send succeeded against an HTTP 401")
	}
}

func TestTelegramSendBotAPINotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok": false, "description": "chat not found"}`))
	}))
	t.Cleanup(srv.Close)
	tg := NewTelegram("T", "1", WithBaseURL(srv.URL))

	if err := tg.Send(context.Background(), "x"); err == nil {
		t.Fatal("Send succeeded against ok=false")
	}
}

func TestTelegramUnconfigured(t *testing.T) {
	for _, tg := range []*Telegram{
		NewTelegram("", ""),
		NewTelegram("token", ""),
		NewTelegram("", "chat"),
	} {
		if tg.Configured() {
			t.Errorf("Configured() = true for %+v", tg)
		}
		if err := tg.Send(context.Background(), "x"); err == nil {
			t.Error("Send succeeded without credentials")
		}
	}
}

func TestTelegramFromEnv(t *testing.T) {
	t.Setenv(EnvTelegramToken, "ENVTOKEN")
	t.Setenv(EnvTelegramChatID, "77")
	tg := NewTelegramFromEnv()
	if !tg.Configured() {
		t.Error("Configured() = false with env credentials set")
	}

	t.Setenv(EnvTelegramChatID, "")
	if NewTelegramFromEnv().Configured() {
		t.Error("Configured() = true with an empty chat id")
	}
}
