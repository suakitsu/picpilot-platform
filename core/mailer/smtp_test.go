package mailer

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPRequiresTLSAndRejectsHeaderInjection(t *testing.T) {
	for _, options := range []Options{{Host: "smtp.example.invalid", Port: 587, From: "owner@example.invalid", TLSMode: "none"}, {Host: "smtp.example.invalid", Port: 587, From: "owner@example.invalid\r\nBcc: other@example.invalid", TLSMode: "starttls"}} {
		if _, err := New(options); err == nil {
			t.Fatal("unsafe SMTP configuration accepted")
		}
	}
	s, err := New(Options{Host: "smtp.example.invalid", Port: 587, From: "owner@example.invalid", TLSMode: "starttls"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct{ to, purpose, code string }{{"victim@example.invalid\r\nBcc: other@example.invalid", "login", "123456"}, {"user@example.invalid", "other", "123456"}, {"user@example.invalid", "login", "12\r\n34"}} {
		if !errors.Is(s.SendCode(context.Background(), input.to, input.purpose, input.code), ErrUnavailable) {
			t.Fatal("invalid message accepted")
		}
	}
}

func TestSMTPSendsSingleCodeOverTLS(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	completed := make(chan error, 1)
	message := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			completed <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		if _, err = fmt.Fprint(conn, "220 mock-smtp\r\n"); err != nil {
			completed <- err
			return
		}
		var data strings.Builder
		for {
			line, readErr := r.ReadString('\n')
			if readErr != nil {
				completed <- readErr
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO "):
				_, err = fmt.Fprint(conn, "250 mock-smtp\r\n")
			case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
				_, err = fmt.Fprint(conn, "250 OK\r\n")
			case strings.HasPrefix(line, "DATA"):
				_, err = fmt.Fprint(conn, "354 send message\r\n")
				if err != nil {
					completed <- err
					return
				}
				for {
					line, err = r.ReadString('\n')
					if err != nil {
						completed <- err
						return
					}
					if line == ".\r\n" {
						break
					}
					data.WriteString(line)
				}
				_, err = fmt.Fprint(conn, "250 accepted\r\n")
			case strings.HasPrefix(line, "QUIT"):
				_, err = fmt.Fprint(conn, "221 bye\r\n")
				message <- data.String()
				completed <- err
				return
			default:
				completed <- errors.New("unexpected SMTP command")
				return
			}
			if err != nil {
				completed <- err
				return
			}
		}
	}()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	s, err := New(Options{Host: "127.0.0.1", Port: port, From: "noreply@example.invalid", TLSMode: "tls"})
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	s.tlsConfig.RootCAs = x509.NewCertPool()
	s.tlsConfig.RootCAs.AddCert(cert)
	if err = s.SendCode(context.Background(), "owner@example.invalid", "reset_password", "103457"); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("SMTP test did not finish")
	}
	text := <-message
	if strings.Count(text, "103457") != 1 || !strings.Contains(text, "reset_password") || !strings.Contains(text, "expires in 10 minutes") {
		t.Fatal("verification mail content incorrect")
	}
}
