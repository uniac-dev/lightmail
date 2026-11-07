package sender

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	messageMail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-msgauth/dkim"
	bolt "go.etcd.io/bbolt"
)

const testAdmin = "test-only-administrator-token-0123456789"

type fixtureDNS struct {
	mu    sync.RWMutex
	txt   map[string][]string
	mx    []*net.MX
	mxErr error
	ips   []net.IPAddr
}

func (d *fixtureDNS) LookupTXT(name string) ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if value, ok := d.txt[name]; ok {
		return value, nil
	}
	return nil, &net.DNSError{IsNotFound: true, Name: name, Err: "not found"}
}
func (d *fixtureDNS) LookupMX(string) ([]*net.MX, error)        { return d.mx, d.mxErr }
func (d *fixtureDNS) LookupIPAddr(string) ([]net.IPAddr, error) { return d.ips, nil }
func (d *fixtureDNS) set(name string, value []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.txt[name] = value
}

type deliveryFunc func(context.Context, Email, Domain, Recipient) error

func (f deliveryFunc) Deliver(ctx context.Context, e Email, d Domain, r Recipient) error {
	return f(ctx, e, d, r)
}

func fixture(t *testing.T, deliverer Deliverer) (*App, *fixtureDNS, Config) {
	t.Helper()
	dns := &fixtureDNS{txt: map[string][]string{}, mx: []*net.MX{{Host: "mx.example.net.", Pref: 10}}, ips: []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}}
	config := Config{DataDir: t.TempDir(), AdminToken: testAdmin, Hostname: "mail.example.net", PublicIP: "203.0.113.20", RequireTLS: true, Retention: 30 * 24 * time.Hour, Version: "test"}
	if deliverer == nil {
		deliverer = deliveryFunc(func(context.Context, Email, Domain, Recipient) error { return nil })
	}
	app, err := newApp(config, dns, deliverer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app, dns, config
}
func request(t *testing.T, a *App, method, path, token string, body any, headers ...map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, path, input)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for _, values := range headers {
		for k, v := range values {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func data[T any](t *testing.T, w *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if w.Code != status {
		t.Fatalf("HTTP %d instead of %d: %s", w.Code, status, w.Body.String())
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
func verifiedDomain(t *testing.T, a *App, dns *fixtureDNS, name string) Domain {
	t.Helper()
	created := request(t, a, "POST", "/v1/domains", testAdmin, map[string]string{"name": name})
	d := data[Domain](t, created, 201)
	if d.PrivateKey != "" || strings.Contains(created.Body.String(), "private_key") {
		t.Fatal("private signing key crossed the HTTP boundary")
	}
	dns.set(d.Records[0].Name, []string{d.Records[0].Value})
	d = data[Domain](t, request(t, a, "POST", "/v1/domains/"+d.ID+"/verify", testAdmin, nil), 200)
	if d.Status != "verified" {
		t.Fatal("DKIM record was not verified")
	}
	return d
}
func sendingKey(t *testing.T, a *App, name, domainID string) string {
	t.Helper()
	w := request(t, a, "POST", "/v1/api-keys", testAdmin, map[string]string{"name": name, "domain_id": domainID})
	result := data[struct {
		Key   APIKey `json:"key"`
		Token string `json:"token"`
	}](t, w, 201)
	if result.Key.Hash != "" {
		t.Fatal("key hash exposed")
	}
	return result.Token
}
func basicMail() SendRequest {
	return SendRequest{From: "Sender <hello@example.com>", To: []string{"person@example.net"}, Subject: "A test", Text: "Hello from a durable queue."}
}

func TestHTTPAuthorizationDomainVerificationAndIdempotency(t *testing.T) {
	a, dns, _ := fixture(t, nil)
	if got := request(t, a, "GET", "/v1/domains", "", nil).Code; got != 401 {
		t.Fatalf("unauthenticated domains: %d", got)
	}
	keyA := sendingKey(t, a, "Application A", "")
	if got := request(t, a, "POST", "/v1/emails", keyA, basicMail()).Code; got != 422 {
		t.Fatalf("unverified send: %d", got)
	}
	d := verifiedDomain(t, a, dns, "example.com")
	other := verifiedDomain(t, a, dns, "other.example")
	keyB := sendingKey(t, a, "Application B", other.ID)
	if got := request(t, a, "POST", "/v1/emails", keyB, basicMail()).Code; got != 403 {
		t.Fatalf("wrong domain send: %d", got)
	}
	if got := request(t, a, "GET", "/v1/api-keys", keyA, nil).Code; got != 403 {
		t.Fatalf("sending key accessed administration: %d", got)
	}
	headers := map[string]string{"Idempotency-Key": "one-message"}
	first := data[Email](t, request(t, a, "POST", "/v1/emails", keyA, basicMail(), headers), 202)
	repeated := data[Email](t, request(t, a, "POST", "/v1/emails", keyA, basicMail(), headers), 202)
	if first.ID != repeated.ID {
		t.Fatal("idempotent admission created another message")
	}
	changed := basicMail()
	changed.Text = "Different body"
	if got := request(t, a, "POST", "/v1/emails", keyA, changed, headers).Code; got != 409 {
		t.Fatalf("idempotency mismatch: %d", got)
	}
	if got := request(t, a, "GET", "/v1/emails/"+first.ID, keyB, nil).Code; got != 404 {
		t.Fatalf("key saw another key's message: %d", got)
	}
	listing := request(t, a, "GET", "/v1/emails", keyB, nil)
	if len(data[[]Email](t, listing, 200)) != 0 {
		t.Fatal("message list leaked between keys")
	}
	adminList := data[[]Email](t, request(t, a, "GET", "/v1/emails", testAdmin, nil), 200)
	if len(adminList) != 1 || adminList[0].Request.Text != "" {
		t.Fatal("message index should contain one lightweight summary")
	}
	if got := request(t, a, "DELETE", "/v1/domains/"+d.ID, testAdmin, nil).Code; got != 409 {
		t.Fatalf("removed domain with queued mail: %d", got)
	}
	keys := request(t, a, "GET", "/v1/api-keys", testAdmin, nil)
	if strings.Contains(keys.Body.String(), keyA) || strings.Contains(keys.Body.String(), tokenHash(keyA)) {
		t.Fatal("API key secret exposed by read")
	}
	canceled := data[Email](t, request(t, a, "POST", "/v1/emails/"+first.ID+"/cancel", keyA, nil), 200)
	if canceled.Status != "canceled" {
		t.Fatal(canceled.Status)
	}
	data[map[string]bool](t, request(t, a, "DELETE", "/v1/domains/"+d.ID, testAdmin, nil), 200)
}

func TestDNSVerificationRejectsWrongMultipleAndTestingKeys(t *testing.T) {
	a, dns, _ := fixture(t, nil)
	d := data[Domain](t, request(t, a, "POST", "/v1/domains", testAdmin, map[string]string{"name": "example.com"}), 201)
	for _, values := range [][]string{{"v=DKIM1; k=rsa; p=wrong"}, {d.Records[0].Value, d.Records[0].Value}, {d.Records[0].Value + "; t=y"}} {
		dns.set(d.Records[0].Name, values)
		checked := data[Domain](t, request(t, a, "POST", "/v1/domains/"+d.ID+"/verify", testAdmin, nil), 200)
		if checked.Status != "pending" {
			t.Fatal("unusable signing record accepted")
		}
	}
}

func TestRestartRecoversQueuedAndInterruptedRecipients(t *testing.T) {
	a, dns, config := fixture(t, nil)
	verifiedDomain(t, a, dns, "example.com")
	e, err := a.submit(basicMail(), identity{Owner: "admin", Admin: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	work, _, err := a.claim()
	if err != nil || work == nil {
		t.Fatalf("claim: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := newApp(config, dns, deliveryFunc(func(context.Context, Email, Domain, Recipient) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	got := data[Email](t, request(t, restarted, "GET", "/v1/emails/"+e.ID, testAdmin, nil), 200)
	if got.Status != "queued" || len(got.Recipients[0].Attempts) != 1 || got.Recipients[0].Attempts[0].Status != "interrupted" {
		t.Fatalf("recovery lost outcome: %+v", got.Recipients)
	}
	claimed, _, err := restarted.claim()
	if err != nil || claimed == nil {
		t.Fatalf("recovered work was not scheduled: %v", err)
	}
}

func TestRecipientOutcomesAndManualRetryNeverResendAcceptedMail(t *testing.T) {
	a, dns, _ := fixture(t, nil)
	verifiedDomain(t, a, dns, "example.com")
	req := basicMail()
	req.To = []string{"accepted@example.net", "rejected@example.net", "later@example.net"}
	e, err := a.submit(req, identity{Owner: "admin", Admin: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		w, _, err := a.claim()
		if err != nil || w == nil {
			t.Fatalf("claim: %v", err)
		}
		var outcome error
		switch w.email.Recipients[w.recipient].Address {
		case "rejected@example.net":
			outcome = &deliveryError{"SMTP 550: no such user", false}
		case "later@example.net":
			outcome = &deliveryError{"SMTP 451: retry later", true}
		}
		if err := a.finish(w, outcome); err != nil {
			t.Fatal(err)
		}
	}
	got := data[Email](t, request(t, a, "GET", "/v1/emails/"+e.ID, testAdmin, nil), 200)
	if got.Recipients[0].Status != "accepted" || got.Recipients[1].Status != "failed" || got.Recipients[2].Status != "queued" {
		t.Fatalf("wrong recipient outcomes: %+v", got.Recipients)
	}
	if _, err := a.changeEmail(e.ID, "cancel", identity{Admin: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.changeEmail(e.ID, "retry", identity{Admin: true}); err != nil {
		t.Fatal(err)
	}
	w, _, err := a.claim()
	if err != nil || w == nil {
		t.Fatalf("manual retry not scheduled: %v", err)
	}
	if w.email.Recipients[w.recipient].Address != "rejected@example.net" {
		t.Fatal("manual retry selected an accepted or canceled recipient")
	}
	if err := a.finish(w, nil); err != nil {
		t.Fatal(err)
	}
	next, _, err := a.claim()
	if err != nil || next != nil {
		t.Fatal("obsolete queue entry selected more work")
	}
}

func TestSessionCookieAndCrossSiteProtection(t *testing.T) {
	a, _, _ := fixture(t, nil)
	login := request(t, a, "POST", "/v1/session", "", map[string]string{"token": testAdmin}, map[string]string{"X-Forwarded-Proto": "https"})
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie protections missing")
	}
	r := httptest.NewRequest("POST", "/v1/domains", strings.NewReader("{\"name\":\"example.com\"}"))
	r.AddCookie(cookies[0])
	r.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-site mutation: %d", w.Code)
	}
	a.config.AdminToken = "rotated-administrator-token-0123456789"
	oldSession := httptest.NewRequest("GET", "/v1/status", nil)
	oldSession.AddCookie(cookies[0])
	rotated := httptest.NewRecorder()
	a.Handler().ServeHTTP(rotated, oldSession)
	if rotated.Code != 401 {
		t.Fatal("administrator rotation did not invalidate existing sessions")
	}
	a.config.AdminToken = testAdmin
	logout := httptest.NewRequest("DELETE", "/v1/session", nil)
	logout.AddCookie(cookies[0])
	out := httptest.NewRecorder()
	a.Handler().ServeHTTP(out, logout)
	check := httptest.NewRequest("GET", "/v1/status", nil)
	check.AddCookie(cookies[0])
	out = httptest.NewRecorder()
	a.Handler().ServeHTTP(out, check)
	if out.Code != 401 {
		t.Fatal("signed-out session remained valid")
	}
}

func TestSignedMIMEAndDirectSMTPAcceptanceWithImplicitMX(t *testing.T) {
	a, dns, config := fixture(t, nil)
	d := verifiedDomain(t, a, dns, "example.com")
	var private Domain
	if err := a.store.db.View(func(tx *bolt.Tx) error { var err error; private, err = load[Domain](tx, "domains", d.ID); return err }); err != nil {
		t.Fatal(err)
	}
	req := basicMail()
	req.BCC = []string{"hidden@example.net"}
	req.HTML = "<h1>Hello</h1>"
	req.Attachments = []Attachment{{Filename: "hello.txt", Content: base64.StdEncoding.EncodeToString([]byte("attachment content")), ContentType: "text/plain"}}
	e, err := a.submit(req, identity{Owner: "admin", Admin: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	captured := make(chan []byte, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		tp := textproto.NewConn(conn)
		_ = tp.PrintfLine("220 mx.example.net ESMTP")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				_ = tp.PrintfLine("250 mx.example.net")
			case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
				_ = tp.PrintfLine("250 OK")
			case line == "DATA":
				_ = tp.PrintfLine("354 Continue")
				raw, err := io.ReadAll(tp.DotReader())
				if err != nil {
					return
				}
				captured <- raw
				_ = tp.PrintfLine("250 queued")
			case line == "QUIT":
				_ = tp.PrintfLine("500 QUIT failed")
				return
			default:
				_ = tp.PrintfLine("500 unexpected command")
			}
		}
	}()
	config.RequireTLS = false
	dns.mx = nil
	dns.mxErr = &net.DNSError{IsNotFound: true, Err: "no MX records"}
	smtpDelivery := newSMTP(config, dns)
	smtpDelivery.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := smtpDelivery.Deliver(ctx, e, private, e.Recipients[0]); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	select {
	case raw = <-captured:
	case <-ctx.Done():
		t.Fatal("SMTP fixture never received DATA")
	}
	verifications, err := dkim.VerifyWithOptions(bytes.NewReader(raw), &dkim.VerifyOptions{LookupTXT: dns.LookupTXT})
	if err != nil || len(verifications) != 1 || verifications[0].Err != nil {
		t.Fatalf("signature did not verify: %v %+v", err, verifications)
	}
	reader, err := messageMail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if reader.Header.Get("Bcc") != "" {
		t.Fatal("BCC recipients exposed in the message headers")
	}
	parts := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(part.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) == 0 {
			t.Fatal("empty MIME part")
		}
		parts++
	}
	if parts != 3 {
		t.Fatalf("expected text, HTML and attachment; got %d", parts)
	}
	<-done
}

func TestPrivateMXIsRejectedBeforeDial(t *testing.T) {
	a, dns, config := fixture(t, nil)
	d := verifiedDomain(t, a, dns, "example.com")
	var private Domain
	_ = a.store.db.View(func(tx *bolt.Tx) error { var err error; private, err = load[Domain](tx, "domains", d.ID); return err })
	e, err := a.submit(basicMail(), identity{Owner: "admin", Admin: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	dns.ips = []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}
	delivery := newSMTP(config, dns)
	delivery.dial = func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dialed a metadata/private destination")
		return nil, fmt.Errorf("unexpected dial")
	}
	if err := delivery.Deliver(context.Background(), e, private, e.Recipients[0]); err == nil {
		t.Fatal("private MX accepted")
	}
}

func TestHeaderInjectionAndInvalidAttachmentsAreRejected(t *testing.T) {
	a, dns, _ := fixture(t, nil)
	verifiedDomain(t, a, dns, "example.com")
	for _, change := range []func(*SendRequest){
		func(r *SendRequest) { r.Subject = "hello\r\nBcc: hidden@example.net" },
		func(r *SendRequest) { r.From = "hello@example.com\r\nCc: hidden@example.net" },
		func(r *SendRequest) { r.Attachments = []Attachment{{Filename: "file.txt", Content: "not base64"}} },
	} {
		r := basicMail()
		change(&r)
		if got := request(t, a, "POST", "/v1/emails", testAdmin, r).Code; got != 422 {
			t.Fatalf("invalid message admitted: %d", got)
		}
	}
}
