package notification

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type EmailConfig struct {
	SMTPHost   string `json:"smtp_host"`
	SMTPPort   int    `json:"smtp_port"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	From       string `json:"from"`
	To         string `json:"to"`
	Encryption string `json:"encryption"`
}

type smtpClient interface {
	StartTLS(*tls.Config) error
	Auth(smtp.Auth) error
	Mail(string) error
	Rcpt(string) error
	Data() (io.WriteCloser, error)
	Quit() error
	Close() error
}

type smtpDialer interface {
	Dial(context.Context, string, string, bool) (smtpClient, error)
}

type networkSMTPDialer struct{}

func (networkSMTPDialer) Dial(ctx context.Context, address, host string, implicitTLS bool) (smtpClient, error) {
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	var connection net.Conn
	var err error
	if implicitTLS {
		dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: tlsConfig}
		connection, err = dialer.DialContext(ctx, "tcp", address)
	} else {
		connection, err = (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to SMTP server: %w", err)
	}
	_ = connection.SetDeadline(time.Now().Add(30 * time.Second))
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		connection.Close()
		return nil, fmt.Errorf("start SMTP client: %w", err)
	}
	return client, nil
}

func parseEmailConfig(config string) (EmailConfig, error) {
	var cfg EmailConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return EmailConfig{}, model.NewValidationError("email config must be valid JSON")
	}
	cfg.SMTPHost = strings.TrimSpace(cfg.SMTPHost)
	cfg.Encryption = strings.ToLower(strings.TrimSpace(cfg.Encryption))
	if cfg.Encryption == "" {
		cfg.Encryption = "starttls"
	}
	if cfg.SMTPPort == 0 {
		if cfg.Encryption == "tls" {
			cfg.SMTPPort = 465
		} else {
			cfg.SMTPPort = 587
		}
	}
	if cfg.SMTPHost == "" || cfg.From == "" || cfg.To == "" {
		return EmailConfig{}, model.NewValidationError("email smtp_host, from, and to are required")
	}
	if cfg.SMTPPort < 1 || cfg.SMTPPort > 65535 {
		return EmailConfig{}, model.NewValidationError("email smtp_port must be between 1 and 65535")
	}
	if cfg.Encryption != "starttls" && cfg.Encryption != "tls" && cfg.Encryption != "none" {
		return EmailConfig{}, model.NewValidationError("email encryption must be starttls, tls, or none")
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return EmailConfig{}, model.NewValidationError("email username and password must be provided together")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return EmailConfig{}, model.NewValidationError("email from address is invalid")
	}
	if _, err := mail.ParseAddress(cfg.To); err != nil {
		return EmailConfig{}, model.NewValidationError("email recipient address is invalid")
	}
	return cfg, nil
}

// SendEmail sends a notification through the configured SMTP server.
func SendEmail(config, message string) error {
	return sendEmailWithDialer(context.Background(), config, message, networkSMTPDialer{})
}

func sendEmailWithDialer(ctx context.Context, config, message string, dialer smtpDialer) error {
	cfg, err := parseEmailConfig(config)
	if err != nil {
		return err
	}
	from, _ := mail.ParseAddress(cfg.From)
	to, _ := mail.ParseAddress(cfg.To)
	address := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))
	client, err := dialer.Dial(ctx, address, cfg.SMTPHost, cfg.Encryption == "tls")
	if err != nil {
		return err
	}
	defer client.Close()
	tlsConfig := &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12}
	if cfg.Encryption == "starttls" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("enable SMTP STARTTLS: %w", err)
		}
	}
	if cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)); err != nil {
			return fmt.Errorf("authenticate with SMTP server: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	body := "From: " + cfg.From + "\r\nTo: " + cfg.To + "\r\nSubject: Jenderal Panel Notification\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + message + "\r\n"
	if _, err := io.WriteString(writer, body); err != nil {
		writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("close SMTP session: %w", err)
	}
	return nil
}
