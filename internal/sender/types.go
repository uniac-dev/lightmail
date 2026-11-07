package sender

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"time"
)

type Config struct {
	Address      string
	DataDir      string
	AdminToken   string
	Hostname     string
	PublicIP     string
	EnvelopeFrom string
	RequireTLS   bool
	Retention    time.Duration
	Version      string
}

type DNS interface {
	LookupTXT(string) ([]string, error)
	LookupMX(string) ([]*net.MX, error)
	LookupIPAddr(string) ([]net.IPAddr, error)
}

type DNSRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
}

type Domain struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Selector   string      `json:"selector"`
	Status     string      `json:"status"`
	CreatedAt  time.Time   `json:"created_at"`
	VerifiedAt *time.Time  `json:"verified_at,omitempty"`
	Records    []DNSRecord `json:"records"`
	PrivateKey string      `json:"private_key,omitempty"`
}

func (d Domain) public() Domain { d.PrivateKey = ""; return d }

type APIKey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	DomainID  string     `json:"domain_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used_at,omitempty"`
	Hash      string     `json:"hash,omitempty"`
}

func (k APIKey) public() APIKey { k.Hash = ""; return k }

type Attachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
}

type SendRequest struct {
	From        string       `json:"from"`
	To          []string     `json:"to"`
	CC          []string     `json:"cc,omitempty"`
	BCC         []string     `json:"bcc,omitempty"`
	ReplyTo     string       `json:"reply_to,omitempty"`
	Subject     string       `json:"subject"`
	Text        string       `json:"text,omitempty"`
	HTML        string       `json:"html,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type Attempt struct {
	At     time.Time `json:"at"`
	Status string    `json:"status"`
	Detail string    `json:"detail,omitempty"`
}

type Recipient struct {
	Address     string    `json:"address"`
	Status      string    `json:"status"`
	Attempts    []Attempt `json:"attempts"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error,omitempty"`
}

type Email struct {
	ID         string      `json:"id"`
	DomainID   string      `json:"domain_id"`
	Owner      string      `json:"owner,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	Status     string      `json:"status"`
	MessageID  string      `json:"message_id"`
	Request    SendRequest `json:"message"`
	Recipients []Recipient `json:"recipients"`
}

func (e Email) public() Email { e.Owner = ""; return e }

func (e *Email) summarize() {
	counts := map[string]int{}
	for _, r := range e.Recipients {
		counts[r.Status]++
	}
	switch {
	case counts["sending"] > 0:
		e.Status = "sending"
	case counts["queued"] > 0:
		e.Status = "queued"
	case counts["accepted"] == len(e.Recipients):
		e.Status = "accepted"
	case counts["accepted"] > 0:
		e.Status = "partially_accepted"
	case counts["failed"] > 0:
		e.Status = "failed"
	default:
		e.Status = "canceled"
	}
	e.UpdatedAt = time.Now().UTC()
}

type problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (p *problem) Error() string                 { return p.Message }
func bad(code, message string, status int) error { return &problem{code, message, status} }

var errMissing = errors.New("record not found")

func identifier(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b[:])
}
