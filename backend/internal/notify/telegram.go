package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Environment variables carrying the Telegram credentials. They are the ONLY
// source of the token and chat id (API contract v2, F-015): the HTTP API
// never reads or writes them. In k8s they come from the VaultStaticSecret
// secret/apps/dps150-web/telegram.
const (
	EnvTelegramToken  = "DPS_TELEGRAM_TOKEN"
	EnvTelegramChatID = "DPS_TELEGRAM_CHAT_ID"
)

// DefaultBaseURL is the production Telegram Bot API endpoint; tests override
// it with WithBaseURL.
const DefaultBaseURL = "https://api.telegram.org"

// defaultSendTimeout bounds one sendMessage HTTP round trip.
const defaultSendTimeout = 10 * time.Second

// TelegramOption configures a Telegram client.
type TelegramOption func(*Telegram)

// WithBaseURL overrides the Bot API base URL (default DefaultBaseURL).
// Intended for tests against an httptest server.
func WithBaseURL(u string) TelegramOption {
	return func(t *Telegram) { t.baseURL = u }
}

// WithHTTPClient overrides the HTTP client used for Bot API calls.
func WithHTTPClient(c *http.Client) TelegramOption {
	return func(t *Telegram) { t.client = c }
}

// Telegram sends messages through the Telegram Bot API with plain net/http.
type Telegram struct {
	token   string
	chatID  string
	baseURL string
	client  *http.Client
}

// NewTelegram builds a client for the given credentials. Empty token or
// chat id leave the client unconfigured: Configured reports false and Send
// fails.
func NewTelegram(token, chatID string, opts ...TelegramOption) *Telegram {
	t := &Telegram{
		token:   token,
		chatID:  chatID,
		baseURL: DefaultBaseURL,
		client:  &http.Client{Timeout: defaultSendTimeout},
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// NewTelegramFromEnv builds a client from EnvTelegramToken and
// EnvTelegramChatID. With either variable empty the client is unconfigured
// and the notifier stays silent.
func NewTelegramFromEnv(opts ...TelegramOption) *Telegram {
	return NewTelegram(os.Getenv(EnvTelegramToken), os.Getenv(EnvTelegramChatID), opts...)
}

// Configured reports whether both the token and the chat id are set.
func (t *Telegram) Configured() bool {
	return t.token != "" && t.chatID != ""
}

// Send posts one sendMessage call with the given text to the configured
// chat. It returns an error for an unconfigured client, a transport failure
// or a non-OK Bot API answer.
func (t *Telegram) Send(ctx context.Context, text string) error {
	if !t.Configured() {
		return fmt.Errorf("telegram: not configured (set %s and %s)",
			EnvTelegramToken, EnvTelegramChatID)
	}
	body, err := json.Marshal(map[string]string{
		"chat_id": t.chatID,
		"text":    text,
	})
	if err != nil {
		return fmt.Errorf("telegram: marshal request: %w", err)
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", t.baseURL, t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: sendMessage: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bot API errors carry a JSON {"ok": false, "description": "..."} body;
	// keep a bounded excerpt for the log.
	if resp.StatusCode != http.StatusOK {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("telegram: sendMessage: HTTP %d: %s", resp.StatusCode, excerpt)
	}
	var answer struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		return fmt.Errorf("telegram: sendMessage: decode answer: %w", err)
	}
	if !answer.OK {
		return fmt.Errorf("telegram: sendMessage: bot API not ok: %s", answer.Description)
	}
	return nil
}
