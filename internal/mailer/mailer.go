package mailer

import (
	"fmt"
	"net/smtp"
	"strings"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/config"
)

// Mailer defines the interface for sending emails.
type Mailer interface {
	Send(to, subject, body string) error
}

// NewMailer returns the appropriate Mailer implementation according to configuration.
func NewMailer(cfg *config.Config) (Mailer, error) {
	switch strings.ToLower(cfg.Mailer) {
	case "smtp":
		if cfg.SMTPHost == "" {
			return nil, fmt.Errorf("SMTP_HOST is required when MAILER=smtp")
		}
		return NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom), nil
	case "mailpit":
		return NewMailpitMailer("localhost:1025", cfg.SMTPFrom), nil
	default:
		// Default to mailpit for local development
		return NewMailpitMailer("localhost:1025", cfg.SMTPFrom), nil
	}
}

// buildMessage formats an RFC 822 email message.
func buildMessage(from, to, subject, body string) []byte {
	headers := make(map[string]string)
	headers["From"] = from
	headers["To"] = to
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/plain; charset=UTF-8"

	var sb strings.Builder
	for k, v := range headers {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	sb.WriteString("\r\n")
	sb.WriteString(body)

	return []byte(sb.String())
}

// SMTPMailer sends emails via a standard SMTP server.
type SMTPMailer struct {
	addr string
	auth smtp.Auth
	from string
}

func NewSMTPMailer(host, port, username, password, from string) *SMTPMailer {
	addr := fmt.Sprintf("%s:%s", host, port)
	var auth smtp.Auth
	if username != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}
	return &SMTPMailer{
		addr: addr,
		auth: auth,
		from: from,
	}
}

func (m *SMTPMailer) Send(to, subject, body string) error {
	msg := buildMessage(m.from, to, subject, body)
	return smtp.SendMail(m.addr, m.auth, m.from, []string{to}, msg)
}

// MailpitMailer sends emails to a local Mailpit instance at localhost:1025 without auth.
type MailpitMailer struct {
	addr string
	from string
}

func NewMailpitMailer(addr, from string) *MailpitMailer {
	if addr == "" {
		addr = "localhost:1025"
	}
	if from == "" {
		from = "noreply@spill.local"
	}
	return &MailpitMailer{
		addr: addr,
		from: from,
	}
}

func (m *MailpitMailer) Send(to, subject, body string) error {
	msg := buildMessage(m.from, to, subject, body)
	// Mailpit requires no authentication
	return smtp.SendMail(m.addr, nil, m.from, []string{to}, msg)
}

// MockMailer records sent messages in memory for unit testing without network.
type MockMailer struct {
	SentMessages []SentMessage
}

type SentMessage struct {
	To      string
	Subject string
	Body    string
}

func NewMockMailer() *MockMailer {
	return &MockMailer{
		SentMessages: make([]SentMessage, 0),
	}
}

func (m *MockMailer) Send(to, subject, body string) error {
	m.SentMessages = append(m.SentMessages, SentMessage{
		To:      to,
		Subject: subject,
		Body:    body,
	})
	return nil
}
