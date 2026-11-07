package sender

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// A test-only resolver publishes the generated records without changing DNS.
type generatedDNS struct{ app *App }

func (d *generatedDNS) LookupTXT(name string) ([]string, error) {
	var values []string
	err := d.app.store.db.View(func(tx *bolt.Tx) error {
		domains, err := list[Domain](tx, "domains")
		if err != nil {
			return err
		}
		for _, domain := range domains {
			for _, record := range domain.Records {
				if record.Name == name {
					values = append(values, record.Value)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, &net.DNSError{IsNotFound: true, Name: name, Err: "not found"}
	}
	return values, nil
}
func (d *generatedDNS) LookupMX(string) ([]*net.MX, error)        { return nil, nil }
func (d *generatedDNS) LookupIPAddr(string) ([]net.IPAddr, error) { return nil, nil }

func TestTypeScriptSDKAgainstHTTPServer(t *testing.T) {
	if _, err := os.Stat("../../sdk/dist/index.js"); err != nil {
		t.Skip("Build the SDK with npm run build --workspace sdk to run cross-runtime integration")
	}
	dns := &generatedDNS{}
	a, err := newApp(Config{DataDir: t.TempDir(), AdminToken: testAdmin, Hostname: "mail.example.net", RequireTLS: true}, dns, deliveryFunc(func(context.Context, Email, Domain, Recipient) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	dns.app = a
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	command := exec.Command("node", "../../sdk/test/real-server.mjs")
	command.Env = append(os.Environ(), "SENDER_TEST_URL="+server.URL, "SENDER_TEST_TOKEN="+testAdmin)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("SDK integration failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}

// TestDashboardFixture runs only with an explicit local fixture address. It is
// excluded from the shipped executable and never sends an internet message.
func TestDashboardFixture(t *testing.T) {
	listen := os.Getenv("SENDER_FIXTURE_ADDRESS")
	if listen == "" {
		t.Skip("local browser fixture is disabled")
	}
	if !strings.HasPrefix(listen, "127.0.0.1:") {
		t.Fatal("the browser fixture must bind to loopback")
	}
	dir := os.Getenv("SENDER_FIXTURE_DATA")
	if dir == "" {
		dir = t.TempDir()
	}
	dns := &generatedDNS{}
	a, err := newApp(Config{DataDir: dir, AdminToken: testAdmin, Hostname: "mail.example.com", PublicIP: "203.0.113.20", RequireTLS: true, Version: "0.2.0"}, dns, deliveryFunc(func(context.Context, Email, Domain, Recipient) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	dns.app = a
	domain, err := a.createDomain("example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.verifyDomain(domain.ID); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct{ subject, to, status string }{
		{"Welcome to your workspace", "alex@example.net", "accepted"},
		{"Your uptime monitor is healthy", "andrew@example.net", "accepted"},
		{"Your weekly activity summary", "team@example.net", "accepted"},
		{"Your sign-in code is 482 906", "alex@example.net", "failed"},
		{"Your deployment is ready", "engineering@example.net", "queued"},
	} {
		req := basicMail()
		req.Subject = sample.subject
		req.To = []string{sample.to}
		req.HTML = "<html><body style=\"font-family:system-ui;padding:36px;color:#18181b\"><h1>" + sample.subject + "</h1><p>Your email is running on your own infrastructure.</p><p>— The team</p></body></html>"
		email, err := a.submit(req, identity{Owner: "admin", Admin: true}, "")
		if err != nil {
			t.Fatal(err)
		}
		err = a.store.db.Update(func(tx *bolt.Tx) error {
			stored, err := load[Email](tx, "emails", email.ID)
			if err != nil {
				return err
			}
			stored.Recipients[0].Status = sample.status
			if sample.status != "queued" {
				detail := ""
				if sample.status == "failed" {
					detail = "SMTP 550: recipient address not found"
					stored.Recipients[0].LastError = detail
				}
				stored.Recipients[0].Attempts = []Attempt{{time.Now().UTC(), sample.status, detail}}
			}
			return updateEmail(tx, &stored)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	server := &http.Server{Addr: listen, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() { <-ctx.Done(); _ = server.Shutdown(context.Background()) }()
	t.Logf("Browser fixture at http://%s, data directory %s", listen, filepath.Base(dir))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
