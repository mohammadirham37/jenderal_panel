package notification

import (
	"encoding/json"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestNotificationDTOUsesObjectConfigAndDefaultsEnabled(t *testing.T) {
	req := CreateChannelRequest{Type: "webhook", Config: json.RawMessage(`{"url":"https://example.com"}`)}
	channel, err := req.Channel()
	if err != nil || !channel.Enabled {
		t.Fatalf("Channel() = %#v, %v", channel, err)
	}
	response, err := channelResponse(model.NotificationChannel{Config: channel.Config})
	if err != nil || response.Config["url"] != "https://example.com" {
		t.Fatalf("channelResponse() = %#v, %v", response, err)
	}
}

func TestNotificationUpdateMergesOnlyPresentFields(t *testing.T) {
	enabled := false
	current := model.NotificationChannel{Type: "telegram", Config: `{"bot_token":"secret","chat_id":"1"}`, Enabled: true}
	got, err := (UpdateChannelRequest{Enabled: &enabled}).Merge(current)
	if err != nil || got.Enabled || got.Type != current.Type || got.Config != current.Config {
		t.Fatalf("Merge() = %#v, %v", got, err)
	}
}
