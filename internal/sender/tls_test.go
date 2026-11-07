package sender

import (
	"context"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestTLSRequiredRejectsServerBeforeSendingEnvelope(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	observed := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		_ = serverConn.SetDeadline(time.Now().Add(3 * time.Second))
		tp := textproto.NewConn(serverConn)
		_ = tp.PrintfLine("220 mx.example.net ESMTP")
		line, err := tp.ReadLine()
		if err != nil || !strings.HasPrefix(line, "EHLO ") {
			observed <- fmt.Errorf("expected EHLO, got %q: %v", line, err)
			return
		}
		_ = tp.PrintfLine("250 mx.example.net")
		line, err = tp.ReadLine()
		if err == nil {
			observed <- fmt.Errorf("sent %q after TLS was unavailable", line)
			return
		}
		observed <- nil
	}()
	smtp := newSMTP(Config{Hostname: "mail.example.com", RequireTLS: true}, nil)
	smtp.dial = func(context.Context, string, string) (net.Conn, error) {
		return clientConn, nil
	}
	err := smtp.transaction(context.Background(), "mx.example.net", "203.0.113.2",
		Email{Request: basicMail()}, "person@example.net", []byte("message"))
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected TLS policy failure, got %v", err)
	}
	if err := <-observed; err != nil {
		t.Fatal(err)
	}
}
