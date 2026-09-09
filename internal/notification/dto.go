package notification

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type ChannelResponse struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Config    map[string]any `json:"config"`
	Enabled   bool           `json:"enabled"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type CreateChannelRequest struct {
	Type    string          `json:"type"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled"`
}

func (r CreateChannelRequest) Channel() (model.NotificationChannel, error) {
	config, err := compactObject(r.Config)
	if err != nil {
		return model.NotificationChannel{}, err
	}
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	return model.NotificationChannel{Type: r.Type, Config: config, Enabled: enabled}, nil
}

type UpdateChannelRequest struct {
	Type    *string         `json:"type"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled"`
}

func (r UpdateChannelRequest) Merge(channel model.NotificationChannel) (model.NotificationChannel, error) {
	if r.Type != nil {
		channel.Type = *r.Type
	}
	if len(r.Config) > 0 {
		config, err := compactObject(r.Config)
		if err != nil {
			return model.NotificationChannel{}, err
		}
		channel.Config = config
	}
	if r.Enabled != nil {
		channel.Enabled = *r.Enabled
	}
	return channel, nil
}

func compactObject(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", model.NewValidationError("config is required")
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return "", model.NewValidationError("config must be a JSON object")
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return "", fmt.Errorf("encode notification config: %w", err)
	}
	return string(encoded), nil
}

func channelResponse(channel model.NotificationChannel) (ChannelResponse, error) {
	var config map[string]any
	if err := json.Unmarshal([]byte(channel.Config), &config); err != nil {
		return ChannelResponse{}, fmt.Errorf("decode notification config: %w", err)
	}
	return ChannelResponse{ID: channel.ID, Type: channel.Type, Config: config, Enabled: channel.Enabled, CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt}, nil
}
