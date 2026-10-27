// Package mailer sends verification messages over authenticated TLS SMTP.
package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("email delivery unavailable")

type Options struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLSMode  string
}

type Sender interface {
	SendCode(context.Context, string, string, string) error
}

type SMTP struct {
	options   Options
	tlsConfig *tls.Config
}

func New(options Options) (*SMTP, error) {
	if options.Host == "" {
		return nil, nil
	}
	a, err := mail.ParseAddress(options.From)
	if err != nil || a.Address != options.From || options.Port < 1 || options.Port > 65535 || strings.ContainsAny(options.Host+options.From, "\r\n") || (options.TLSMode != "tls" && options.TLSMode != "starttls") {
		return nil, errors.New("invalid SMTP configuration; TLS and a sender address are required")
	}
	return &SMTP{options: options, tlsConfig: &tls.Config{ServerName: options.Host, MinVersion: tls.VersionTLS12}}, nil
}

func (s *SMTP) SendCode(ctx context.Context, to, purpose, code string) error {
	if s == nil {
		return ErrUnavailable
	}
	a, err := mail.ParseAddress(to)
	if err != nil || a.Address != to || strings.ContainsAny(to, "\r\n") || len(code) != 6 {
		return ErrUnavailable
	}
	for _, v := range code {
		if v < '0' || v > '9' {
			return ErrUnavailable
		}
	}
	if purpose != "register" && purpose != "login" && purpose != "reset_password" {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cfg := s.options
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return ErrUnavailable
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	tlsConfig := s.tlsConfig.Clone()
	if cfg.TLSMode == "tls" {
		tlsConn := tls.Client(conn, tlsConfig)
		if tlsConn.HandshakeContext(ctx) != nil {
			return ErrUnavailable
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return ErrUnavailable
	}
	defer client.Close()
	if cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return ErrUnavailable
		}
		if client.StartTLS(tlsConfig) != nil {
			return ErrUnavailable
		}
	}
	if cfg.Username != "" {
		if client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)) != nil {
			return ErrUnavailable
		}
	}
	if client.Mail(cfg.From) != nil || client.Rcpt(to) != nil {
		return ErrUnavailable
	}
	writer, err := client.Data()
	if err != nil {
		return ErrUnavailable
	}
	message := fmt.Sprintf("From: PicPilot <%s>\r\nTo: %s\r\nSubject: PicPilot verification code\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour PicPilot %s code is: %s\r\n\r\nIt expires in 10 minutes. Do not share this code. If you did not request it, ignore this message.\r\n", cfg.From, to, purpose, code)
	if _, err = writer.Write([]byte(message)); err != nil {
		return ErrUnavailable
	}
	if writer.Close() != nil {
		return ErrUnavailable
	}
	// Once DATA was accepted, a failed QUIT must not invalidate the delivered code.
	_ = client.Quit()
	return nil
}
