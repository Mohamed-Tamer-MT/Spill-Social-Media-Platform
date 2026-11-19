package mailer_test

import (
	"bufio"
	"net"
	"strings"
	"testing"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/config"
	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/mailer"
)

func TestNewMailer(t *testing.T) {
	t.Run("SMTP without host fails", func(t *testing.T) {
		cfg := &config.Config{
			Mailer:   "smtp",
			SMTPHost: "",
		}
		_, err := mailer.NewMailer(cfg)
		if err == nil {
			t.Fatal("expected error when SMTP_HOST is empty for MAILER=smtp, got nil")
		}
	})

	t.Run("SMTP with host succeeds", func(t *testing.T) {
		cfg := &config.Config{
			Mailer:       "smtp",
			SMTPHost:     "smtp.example.com",
			SMTPPort:     "587",
			SMTPUsername: "user",
			SMTPPassword: "password",
			SMTPFrom:     "noreply@example.com",
		}
		m, err := mailer.NewMailer(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := m.(*mailer.SMTPMailer); !ok {
			t.Fatalf("expected *mailer.SMTPMailer, got %T", m)
		}
	})

	t.Run("Mailpit with defaults succeeds", func(t *testing.T) {
		cfg := &config.Config{
			Mailer:   "mailpit",
			SMTPHost: "",
			SMTPPort: "",
		}
		m, err := mailer.NewMailer(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := m.(*mailer.MailpitMailer); !ok {
			t.Fatalf("expected *mailer.MailpitMailer, got %T", m)
		}
	})

	t.Run("Default fallback to mailpit", func(t *testing.T) {
		cfg := &config.Config{
			Mailer:   "unknown_provider",
			SMTPHost: "127.0.0.1",
			SMTPPort: "1025",
		}
		m, err := mailer.NewMailer(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := m.(*mailer.MailpitMailer); !ok {
			t.Fatalf("expected fallback to *mailer.MailpitMailer, got %T", m)
		}
	})
}

func TestMockMailer(t *testing.T) {
	m := mailer.NewMockMailer()
	err := m.Send("user@example.com", "Subject Test", "Body content")
	if err != nil {
		t.Fatalf("unexpected error from mock mailer: %v", err)
	}

	if len(m.SentMessages) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(m.SentMessages))
	}
	msg := m.SentMessages[0]
	if msg.To != "user@example.com" || msg.Subject != "Subject Test" || msg.Body != "Body content" {
		t.Errorf("unexpected message payload: %+v", msg)
	}
}

// startFakeSMTPServer starts a minimal dummy SMTP server for testing Send().
func startFakeSMTPServer(t *testing.T) (net.Listener, chan string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on local port: %v", err)
	}

	received := make(chan string, 1)

	go func() {
		defer close(received)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		w := bufio.NewWriter(conn)
		r := bufio.NewReader(conn)

		_, _ = w.WriteString("220 Welcome to Fake SMTP\r\n")
		_ = w.Flush()

		var dataContent strings.Builder
		inData := false

		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			trimmed := strings.TrimRight(line, "\r\n")

			if inData {
				if trimmed == "." {
					inData = false
					_, _ = w.WriteString("250 OK Message accepted\r\n")
					_ = w.Flush()
					received <- dataContent.String()
					continue
				}
				dataContent.WriteString(line)
				continue
			}

			upper := strings.ToUpper(trimmed)
			if strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO") {
				_, _ = w.WriteString("250-Hello\r\n250 OK\r\n")
				_ = w.Flush()
			} else if strings.HasPrefix(upper, "MAIL FROM:") {
				_, _ = w.WriteString("250 OK\r\n")
				_ = w.Flush()
			} else if strings.HasPrefix(upper, "RCPT TO:") {
				_, _ = w.WriteString("250 OK\r\n")
				_ = w.Flush()
			} else if upper == "DATA" {
				inData = true
				_, _ = w.WriteString("354 Start mail input; end with <CRLF>.<CRLF>\r\n")
				_ = w.Flush()
			} else if upper == "QUIT" {
				_, _ = w.WriteString("221 Bye\r\n")
				_ = w.Flush()
				break
			} else {
				_, _ = w.WriteString("500 Command unrecognized\r\n")
				_ = w.Flush()
			}
		}
	}()

	return ln, received
}

func TestMailpitMailer_Send(t *testing.T) {
	ln, received := startFakeSMTPServer(t)
	defer ln.Close()

	m := mailer.NewMailpitMailer(ln.Addr().String(), "test@spill.local")
	err := m.Send("recipient@example.com", "Your Code: 123456", "Hello, your code is 123456.")
	if err != nil {
		t.Fatalf("failed to send email via MailpitMailer: %v", err)
	}

	raw := <-received
	if !strings.Contains(raw, "Subject: Your Code: 123456") {
		t.Errorf("expected received email to contain subject, got: %s", raw)
	}
	if !strings.Contains(raw, "From: test@spill.local") {
		t.Errorf("expected received email to contain From, got: %s", raw)
	}
	if !strings.Contains(raw, "To: recipient@example.com") {
		t.Errorf("expected received email to contain To, got: %s", raw)
	}
	if !strings.Contains(raw, "Hello, your code is 123456.") {
		t.Errorf("expected received email to contain body, got: %s", raw)
	}
}

func TestSMTPMailer_Send(t *testing.T) {
	ln, received := startFakeSMTPServer(t)
	defer ln.Close()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	m := mailer.NewSMTPMailer(host, port, "", "", "smtp@spill.local")
	err := m.Send("user@domain.com", "SMTP Subject", "SMTP Body")
	if err != nil {
		t.Fatalf("failed to send email via SMTPMailer: %v", err)
	}

	raw := <-received
	if !strings.Contains(raw, "Subject: SMTP Subject") {
		t.Errorf("expected received email to contain subject, got: %s", raw)
	}
	if !strings.Contains(raw, "From: smtp@spill.local") {
		t.Errorf("expected received email to contain From, got: %s", raw)
	}
}
