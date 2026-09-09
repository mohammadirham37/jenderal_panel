package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

var telegramTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(?::[A-Za-z0-9_-]+)?$`)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// WebhookConfig holds the configuration for a webhook channel.
type WebhookConfig struct {
	URL           string `json:"url"`
	Authorization string `json:"authorization"`
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

// SendWebhook sends a JSON message to the configured webhook URL.
func SendWebhook(config, message string) error {
	return sendWebhook(context.Background(), config, message)
}

func sendWebhook(ctx context.Context, config, message string) error {
	var cfg WebhookConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("parse webhook config: %w", err)
	}
	if cfg.URL == "" {
		return fmt.Errorf("webhook url is empty")
	}
	if err := validateHTTPURL(cfg.URL, "webhook url"); err != nil {
		return err
	}

	payload, _ := json.Marshal(map[string]string{"text": message})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Authorization != "" {
		req.Header.Set("Authorization", cfg.Authorization)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// SendTelegram sends a message via the Telegram Bot API.
func SendTelegram(config, message string) error {
	return sendTelegram(context.Background(), config, message)
}

func sendTelegram(ctx context.Context, config, message string) error {
	var cfg TelegramConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("parse telegram config: %w", err)
	}
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return fmt.Errorf("telegram bot_token and chat_id are required")
	}
	if !telegramTokenPattern.MatchString(cfg.BotToken) {
		return model.NewValidationError("telegram bot_token format is invalid")
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken)
	payload, _ := json.Marshal(map[string]string{
		"chat_id": cfg.ChatID,
		"text":    message,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create telegram request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram returned status %d", resp.StatusCode)
	}
	return nil
}

// SendDiscord sends a message via a Discord webhook.
func SendDiscord(config, message string) error {
	return sendDiscord(context.Background(), config, message)
}

func sendDiscord(ctx context.Context, config, message string) error {
	var cfg DiscordConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("parse discord config: %w", err)
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("discord webhook_url is empty")
	}
	if err := validateHTTPURL(cfg.WebhookURL, "discord webhook_url"); err != nil {
		return err
	}

	payload, _ := json.Marshal(map[string]string{"content": message})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create discord request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord returned status %d", resp.StatusCode)
	}
	return nil
}

// ValidateConfig validates a channel using the same schema used for delivery.
func ValidateConfig(channelType, config string) error {
	if config == "" || !json.Valid([]byte(config)) {
		return model.NewValidationError("config must be valid JSON")
	}
	switch channelType {
	case "webhook":
		var cfg WebhookConfig
		if json.Unmarshal([]byte(config), &cfg) != nil || cfg.URL == "" {
			return model.NewValidationError("webhook url is required")
		}
		if err := validateHTTPURL(cfg.URL, "webhook url"); err != nil {
			return err
		}
	case "telegram":
		var cfg TelegramConfig
		if json.Unmarshal([]byte(config), &cfg) != nil || cfg.BotToken == "" || cfg.ChatID == "" {
			return model.NewValidationError("telegram bot_token and chat_id are required")
		}
		if !telegramTokenPattern.MatchString(cfg.BotToken) {
			return model.NewValidationError("telegram bot_token format is invalid")
		}
	case "discord":
		var cfg DiscordConfig
		if json.Unmarshal([]byte(config), &cfg) != nil || cfg.WebhookURL == "" {
			return model.NewValidationError("discord webhook_url is required")
		}
		if err := validateHTTPURL(cfg.WebhookURL, "discord webhook_url"); err != nil {
			return err
		}
	case "email":
		if _, err := parseEmailConfig(config); err != nil {
			return err
		}
	default:
		return model.NewValidationError("type must be email, telegram, discord, or webhook")
	}
	return nil
}

func validateHTTPURL(rawURL, field string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return model.NewValidationError(field + " must be a valid HTTP or HTTPS URL")
	}
	return nil
}
