package notification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// WebhookConfig holds the configuration for a webhook channel.
type WebhookConfig struct {
	URL string `json:"url"`
}

// TelegramConfig holds the configuration for a Telegram channel.
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// DiscordConfig holds the configuration for a Discord webhook channel.
type DiscordConfig struct {
	WebhookURL string `json:"webhook_url"`
}

// EmailConfig holds the configuration for an email channel.
type EmailConfig struct {
	SMTPHost string `json:"smtp_host"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// SendWebhook sends a JSON message to the configured webhook URL.
func SendWebhook(config, message string) error {
	var cfg WebhookConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("parse webhook config: %w", err)
	}
	if cfg.URL == "" {
		return fmt.Errorf("webhook url is empty")
	}

	payload, _ := json.Marshal(map[string]string{"text": message})
	resp, err := httpClient.Post(cfg.URL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("webhook POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// SendTelegram sends a message via the Telegram Bot API.
func SendTelegram(config, message string) error {
	var cfg TelegramConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("parse telegram config: %w", err)
	}
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return fmt.Errorf("telegram bot_token and chat_id are required")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	payload, _ := json.Marshal(map[string]string{
		"chat_id": cfg.ChatID,
		"text":    message,
	})

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}
	return nil
}

// SendDiscord sends a message via a Discord webhook.
func SendDiscord(config, message string) error {
	var cfg DiscordConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("parse discord config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("discord webhook_url is empty")
	}

	payload, _ := json.Marshal(map[string]string{"content": message})
	resp, err := httpClient.Post(cfg.WebhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("discord POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}
	return nil
}

// SendEmail sends a message via SMTP (stub implementation, logs instead of actually sending).
func SendEmail(config, message string) error {
	var cfg EmailConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("parse email config: %w", err)
	}
	if cfg.SMTPHost == "" || cfg.From == "" || cfg.To == "" {
		return fmt.Errorf("email smtp_host, from, and to are required")
	}

	// Stub: log instead of sending via net/smtp
	log.Printf("[notification] email stub: from=%s to=%s host=%s message=%s", cfg.From, cfg.To, cfg.SMTPHost, message)
	return nil
}
