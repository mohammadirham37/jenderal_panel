package notification

import (
	"context"
	"crypto/tls"
	"io"
	"net/smtp"
	"strings"
	"testing"
)

type smtpRecorder struct {
	implicit   bool
	startedTLS bool
	authed     bool
	from       string
	to         string
	body       strings.Builder
}

func (s *smtpRecorder) StartTLS(*tls.Config) error    { s.startedTLS = true; return nil }
func (s *smtpRecorder) Auth(smtp.Auth) error          { s.authed = true; return nil }
func (s *smtpRecorder) Mail(value string) error       { s.from = value; return nil }
func (s *smtpRecorder) Rcpt(value string) error       { s.to = value; return nil }
func (s *smtpRecorder) Data() (io.WriteCloser, error) { return nopWriteCloser{&s.body}, nil }
func (s *smtpRecorder) Quit() error                   { return nil }
func (s *smtpRecorder) Close() error                  { return nil }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

type smtpRecorderDialer struct{ client *smtpRecorder }

func (d smtpRecorderDialer) Dial(context.Context, string, string, bool) (smtpClient, error) {
	return d.client, nil
}

func TestSendEmailUsesStartTLSAndSMTPEnvelope(t *testing.T) {
	client := &smtpRecorder{}
	config := `{"smtp_host":"smtp.example.com","smtp_port":587,"username":"user","password":"secret","from":"panel@example.com","to":"admin@example.com","encryption":"starttls"}`
	if err := sendEmailWithDialer(context.Background(), config, "server recovered", smtpRecorderDialer{client}); err != nil {
		t.Fatal(err)
	}
	if !client.startedTLS || !client.authed || client.from != "panel@example.com" || client.to != "admin@example.com" {
		t.Fatalf("smtp calls: %#v", client)
	}
	if !strings.Contains(client.body.String(), "Subject: Jenderal Panel Notification") || !strings.Contains(client.body.String(), "server recovered") {
		t.Fatalf("message = %q", client.body.String())
	}
}
