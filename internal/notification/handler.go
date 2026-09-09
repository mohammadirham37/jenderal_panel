package notification

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles notification channel HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new notification Handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "notification",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// ListChannels handles GET /api/notification-channels.
func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.svc.ListChannels(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	responses := make([]ChannelResponse, 0, len(channels))
	for _, channel := range channels {
		response, err := channelResponse(channel)
		if err != nil {
			httputil.HandleError(w, err)
			return
		}
		responses = append(responses, response)
	}
	httputil.JSON(w, http.StatusOK, responses)
}

// CreateChannel handles POST /api/notification-channels.
func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	var req CreateChannelRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	channel, err := req.Channel()
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	ch, err := h.svc.CreateChannel(r.Context(), channel)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_notification_channel", ch.ID, "created "+ch.Type+" notification channel")
	response, err := channelResponse(ch)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, response)
}

// UpdateChannel handles PUT /api/notification-channels/{id}.
func (h *Handler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateChannelRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	current, err := h.svc.GetChannel(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	channel, err := req.Merge(current)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := h.svc.UpdateChannel(r.Context(), id, channel); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "update_notification_channel", id, "updated notification channel")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteChannel handles DELETE /api/notification-channels/{id}.
func (h *Handler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteChannel(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_notification_channel", id, "deleted notification channel")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// TestChannel handles POST /api/notification-channels/{id}/test.
func (h *Handler) TestChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.TestChannel(r.Context(), id); err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "TEST_FAILED", err.Error())
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
