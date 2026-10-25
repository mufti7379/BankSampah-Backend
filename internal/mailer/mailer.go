// Package mailer mengirim email transaksional (reset password, dll.)
package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Message adalah satu email yang akan dikirim (multipart: teks + HTML)
type Message struct {
	To       string
	ToName   string
	Subject  string
	TextBody string
	HTMLBody string
}

// Mailer adalah kontrak pengirim email
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Config berisi pengaturan SMTP dari .env
type Config struct {
	Host      string
	Port      int
	Username  string
	Password  string
	FromEmail string
	FromName  string
}

// New mengembalikan SMTPMailer jika SMTP_HOST diisi, atau LogMailer untuk development
func New(cfg Config) Mailer {
	if cfg.Host == "" {
		log.Warn().Msg("SMTP_HOST kosong: email TIDAK dikirim, isinya hanya dicetak ke log (mode development)")
		return &LogMailer{}
	}
	return &SMTPMailer{cfg: cfg}
}

// ---------------------------------------------------------------------------
// LogMailer: mencetak email ke log (tanpa SMTP), untuk development lokal
// ---------------------------------------------------------------------------

type LogMailer struct{}

func (m *LogMailer) Send(_ context.Context, msg Message) error {
	log.Info().
		Str("to", msg.To).
		Str("subject", msg.Subject).
		Msg("[DEV MAILER] Email tidak dikirim, isi pesan:\n" + msg.TextBody)
	return nil
}

// ---------------------------------------------------------------------------
// SMTPMailer: mengirim email via SMTP (port 587 STARTTLS / port 465 TLS)
// ---------------------------------------------------------------------------

type SMTPMailer struct {
	cfg Config
}

const smtpTimeout = 20 * time.Second

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	raw, err := m.buildMessage(msg)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	dialer := &net.Dialer{Timeout: smtpTimeout}
	tlsCfg := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}

	// Port 465 memakai TLS langsung; port lain (587/25) memakai STARTTLS
	var conn net.Conn
	if m.cfg.Port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("gagal terhubung ke SMTP %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(smtpTimeout))
	}

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("gagal membuat klien SMTP: %w", err)
	}
	defer client.Close()

	if m.cfg.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("server SMTP tidak mendukung STARTTLS, koneksi tidak aman ditolak")
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("gagal STARTTLS: %w", err)
		}
	}

	if m.cfg.Username != "" {
		auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("autentikasi SMTP gagal: %w", err)
		}
	}

	if err := client.Mail(m.cfg.FromEmail); err != nil {
		return fmt.Errorf("SMTP MAIL FROM gagal: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("SMTP RCPT TO gagal: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA gagal: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		w.Close()
		return fmt.Errorf("gagal menulis isi email: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("gagal menyelesaikan pengiriman email: %w", err)
	}
	return client.Quit()
}

// buildMessage menyusun email MIME multipart/alternative (teks & HTML)
func (m *SMTPMailer) buildMessage(msg Message) ([]byte, error) {
	if strings.ContainsAny(msg.To+msg.Subject, "\r\n") {
		return nil, fmt.Errorf("header email mengandung karakter baris baru (indikasi header injection)")
	}

	boundaryBytes := make([]byte, 12)
	if _, err := rand.Read(boundaryBytes); err != nil {
		return nil, err
	}
	boundary := "bsk-" + hex.EncodeToString(boundaryBytes)

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s <%s>\r\n", mime.QEncoding.Encode("utf-8", m.cfg.FromName), m.cfg.FromEmail)
	if msg.ToName != "" {
		fmt.Fprintf(&b, "To: %s <%s>\r\n", mime.QEncoding.Encode("utf-8", msg.ToName), msg.To)
	} else {
		fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	}
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n", boundary)
	b.WriteString(msg.TextBody + "\r\n")
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n", boundary)
	b.WriteString(msg.HTMLBody + "\r\n")
	fmt.Fprintf(&b, "--%s--\r\n", boundary)

	return b.Bytes(), nil
}
