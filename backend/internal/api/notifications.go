package api

import (
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/notify"
	"dps150-web/backend/internal/storage"
)

// notificationsState carries the dependencies of the notification-settings
// endpoints, injected by WireNotifications before the router is built.
type notificationsState struct {
	store      notify.SettingsStore // nil when storage is disabled
	configured bool                 // Telegram token and chat id present in env
}

var notificationsDeps atomic.Pointer[notificationsState]

// WireNotifications injects the settings store and the Telegram-configured
// flag consumed by GET/PUT /api/v1/settings/notifications. main.go calls it
// from the stage-2 wiring anchor before api.NewRouter; store may be nil when
// storage is disabled (the endpoints then answer 503 storage_unavailable).
func WireNotifications(store notify.SettingsStore, configured bool) {
	notificationsDeps.Store(&notificationsState{store: store, configured: configured})
}

// registerNotificationRoutes registers the F-015 settings endpoints.
func registerNotificationRoutes(v1 *gin.RouterGroup) {
	v1.GET("/settings/notifications", getNotificationSettings)
	v1.PUT("/settings/notifications", putNotificationSettings)
}

// notificationSettingsDTO is the GET/PUT /api/v1/settings/notifications
// body per the API contract v2 (F-015). Configured is present (false) only
// when the Telegram env variables are not set, and only on GET.
type notificationSettingsDTO struct {
	TelegramEnabled bool                 `json:"telegramEnabled"`
	Events          notify.EventSettings `json:"events"`
	Configured      *bool                `json:"configured,omitempty"`
}

// notificationSettingsRequest is the PUT body: a partial update, absent
// fields keep their current values.
type notificationSettingsRequest struct {
	TelegramEnabled *bool `json:"telegramEnabled"`
	Events          *struct {
		ProtectionTrip  *bool `json:"protectionTrip"`
		DeviceLink      *bool `json:"deviceLink"`
		Output          *bool `json:"output"`
		MeteringSession *bool `json:"meteringSession"`
	} `json:"events"`
}

// getNotificationSettings handles GET /api/v1/settings/notifications.
func getNotificationSettings(c *gin.Context) {
	deps, settings, ok := loadNotificationSettings(c)
	if !ok {
		return
	}
	resp := notificationSettingsDTO{
		TelegramEnabled: settings.TelegramEnabled,
		Events:          settings.Events,
	}
	if !deps.configured {
		configured := false
		resp.Configured = &configured
	}
	c.JSON(http.StatusOK, resp)
}

// putNotificationSettings handles PUT /api/v1/settings/notifications:
// it merges the given fields into the stored settings and answers with the
// resulting settings, mirroring the contract body.
func putNotificationSettings(c *gin.Context) {
	var req notificationSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "bad_request",
			"request body must be a JSON object with telegramEnabled and/or events booleans")
		return
	}
	deps, settings, ok := loadNotificationSettings(c)
	if !ok {
		return
	}
	if req.TelegramEnabled != nil {
		settings.TelegramEnabled = *req.TelegramEnabled
	}
	if req.Events != nil {
		if v := req.Events.ProtectionTrip; v != nil {
			settings.Events.ProtectionTrip = *v
		}
		if v := req.Events.DeviceLink; v != nil {
			settings.Events.DeviceLink = *v
		}
		if v := req.Events.Output; v != nil {
			settings.Events.Output = *v
		}
		if v := req.Events.MeteringSession; v != nil {
			settings.Events.MeteringSession = *v
		}
	}
	if err := notify.SaveSettings(c.Request.Context(), deps.store, settings); err != nil {
		writeNotificationsError(c, err)
		return
	}
	c.JSON(http.StatusOK, notificationSettingsDTO{
		TelegramEnabled: settings.TelegramEnabled,
		Events:          settings.Events,
	})
}

// loadNotificationSettings resolves the wired dependencies and the current
// settings, writing the contract error response and reporting !ok on
// failure.
func loadNotificationSettings(c *gin.Context) (*notificationsState, notify.Settings, bool) {
	deps := notificationsDeps.Load()
	if deps == nil || deps.store == nil {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"storage is not available")
		return nil, notify.Settings{}, false
	}
	settings, err := notify.LoadSettings(c.Request.Context(), deps.store)
	if err != nil {
		writeNotificationsError(c, err)
		return nil, notify.Settings{}, false
	}
	return deps, settings, true
}

// writeNotificationsError maps settings persistence errors onto the
// contract's error responses.
func writeNotificationsError(c *gin.Context, err error) {
	if errors.Is(err, storage.ErrUnavailable) {
		writeError(c, http.StatusServiceUnavailable, "storage_unavailable",
			"storage is not available")
		return
	}
	writeError(c, http.StatusInternalServerError, "internal", err.Error())
}
