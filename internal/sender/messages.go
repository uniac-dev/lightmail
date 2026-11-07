package sender

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"
	"unicode"

	messageMail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-msgauth/dkim"
	bolt "go.etcd.io/bbolt"
)

func address(value string) (*mail.Address, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return nil, bad("invalid_address", "Email addresses must not contain control characters.", 422)
	}
	addr, err := mail.ParseAddress(value)
	if err != nil {
		return nil, bad("invalid_address", "An email address is invalid.", 422)
	}
	at := strings.LastIndexByte(addr.Address, '@')
	if at < 1 {
		return nil, bad("invalid_address", "An email address is invalid.", 422)
	}
	domain, err := domainName(addr.Address[at+1:])
	if err != nil {
		return nil, bad("invalid_address", "Use an email address with a valid public domain.", 422)
	}
	for _, c := range addr.Address[:at] {
		if c > 127 {
			return nil, bad("invalid_address", "Internationalized local parts are not supported.", 422)
		}
	}
	addr.Address = addr.Address[:at+1] + domain
	return addr, nil
}

func validateMessage(req *SendRequest) (string, []string, error) {
	from, err := address(req.From)
	if err != nil {
		return "", nil, err
	}
	req.From = from.String()
	if len(req.To) == 0 || len(req.To)+len(req.CC)+len(req.BCC) > 50 {
		return "", nil, bad("invalid_recipients", "Provide between 1 and 50 recipients, including at least one To recipient.", 422)
	}
	if len(req.Subject) > 998 || strings.ContainsAny(req.Subject, "\r\n\x00") {
		return "", nil, bad("invalid_subject", "The subject must be a single line of at most 998 bytes.", 422)
	}
	if req.Text == "" && req.HTML == "" {
		return "", nil, bad("missing_body", "Provide text or html content.", 422)
	}
	recipients := []string{}
	seen := map[string]bool{}
	for _, group := range [][]string{req.To, req.CC, req.BCC} {
		for i, value := range group {
			parsed, err := address(value)
			if err != nil {
				return "", nil, err
			}
			group[i] = parsed.String()
			if !seen[parsed.Address] {
				recipients = append(recipients, parsed.Address)
				seen[parsed.Address] = true
			}
		}
	}
	if req.ReplyTo != "" {
		reply, err := address(req.ReplyTo)
		if err != nil {
			return "", nil, err
		}
		req.ReplyTo = reply.String()
	}
	size := len(req.Text) + len(req.HTML)
	if len(req.Attachments) > 10 {
		return "", nil, bad("too_many_attachments", "At most 10 attachments are allowed.", 422)
	}
	for i := range req.Attachments {
		attachment := &req.Attachments[i]
		if attachment.Filename == "" || len(attachment.Filename) > 255 || strings.IndexFunc(attachment.Filename, unicode.IsControl) >= 0 {
			return "", nil, bad("invalid_attachment", "Each attachment needs a filename without control characters.", 422)
		}
		content, err := base64.StdEncoding.DecodeString(attachment.Content)
		if err != nil {
			return "", nil, bad("invalid_attachment", "Attachment content must be base64 encoded.", 422)
		}
		size += len(content)
		if attachment.ContentType == "" {
			attachment.ContentType = "application/octet-stream"
		}
		if _, _, err := mime.ParseMediaType(attachment.ContentType); err != nil || strings.ContainsAny(attachment.ContentType, "\r\n") {
			return "", nil, bad("invalid_attachment", "The attachment content type is invalid.", 422)
		}
	}
	if size > 10*1024*1024 {
		return "", nil, bad("message_too_large", "Decoded message content must not exceed 10 MiB.", 413)
	}
	return from.Address[strings.LastIndexByte(from.Address, '@')+1:], recipients, nil
}

type identity struct {
	Owner, DomainID string
	Admin           bool
}
type idempotency struct {
	EmailID string    `json:"email_id"`
	Hash    string    `json:"hash"`
	Expires time.Time `json:"expires"`
}

func (a *App) submit(req SendRequest, caller identity, idempotencyKey string) (Email, error) {
	name, recipients, err := validateMessage(&req)
	if err != nil {
		return Email{}, err
	}
	if len(idempotencyKey) > 256 || strings.IndexFunc(idempotencyKey, unicode.IsControl) >= 0 {
		return Email{}, bad("invalid_idempotency_key", "Idempotency keys must contain at most 256 printable bytes.", 422)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return Email{}, err
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	idemDigest := sha256.Sum256([]byte(caller.Owner + "\x00" + idempotencyKey))
	idemID := hex.EncodeToString(idemDigest[:])
	now := time.Now().UTC()
	e := Email{ID: identifier("em_"), Owner: caller.Owner, CreatedAt: now, UpdatedAt: now, Status: "queued", Request: req, Recipients: []Recipient{}}
	e.MessageID = e.ID + "@" + a.config.Hostname
	for _, recipient := range recipients {
		e.Recipients = append(e.Recipients, Recipient{Address: recipient, Status: "queued", Attempts: []Attempt{}, NextAttempt: now})
	}
	err = a.store.db.Update(func(tx *bolt.Tx) error {
		if idempotencyKey != "" {
			existing, err := load[idempotency](tx, "idempotency", idemID)
			if err == nil && existing.Expires.After(now) {
				if existing.Hash != hash {
					return bad("idempotency_conflict", "This idempotency key was used with different message content.", 409)
				}
				e, err = load[Email](tx, "emails", existing.EmailID)
				return err
			}
			if err != nil && err != errMissing {
				return err
			}
		}
		domains, err := list[Domain](tx, "domains")
		if err != nil {
			return err
		}
		for _, d := range domains {
			if d.Name == name && d.Status == "verified" {
				e.DomainID = d.ID
				break
			}
		}
		if e.DomainID == "" {
			return bad("domain_not_verified", "Verify the From address's domain before sending.", 422)
		}
		if caller.DomainID != "" && caller.DomainID != e.DomainID {
			return bad("domain_not_allowed", "This API key cannot send from that domain.", 403)
		}
		if err := updateEmail(tx, &e); err != nil {
			return err
		}
		if idempotencyKey != "" {
			return save(tx, "idempotency", idemID, idempotency{e.ID, hash, now.Add(24 * time.Hour)})
		}
		return nil
	})
	if err == nil {
		a.wake()
	}
	return e.public(), err
}

func buildMessage(e Email, d Domain) ([]byte, error) {
	var header messageMail.Header
	for name, values := range map[string][]string{"From": {e.Request.From}, "To": e.Request.To, "Cc": e.Request.CC} {
		var addresses []*messageMail.Address
		for _, value := range values {
			parsed, err := mail.ParseAddress(value)
			if err != nil {
				return nil, err
			}
			addresses = append(addresses, parsed)
		}
		if len(addresses) > 0 {
			header.SetAddressList(name, addresses)
		}
	}
	if e.Request.ReplyTo != "" {
		reply, err := mail.ParseAddress(e.Request.ReplyTo)
		if err != nil {
			return nil, err
		}
		header.SetAddressList("Reply-To", []*messageMail.Address{reply})
	}
	header.SetSubject(e.Request.Subject)
	header.SetDate(e.CreatedAt)
	header.SetMessageID(e.MessageID)
	var raw bytes.Buffer
	w, err := messageMail.CreateWriter(&raw, header)
	if err != nil {
		return nil, err
	}
	inline, err := w.CreateInline()
	if err != nil {
		return nil, err
	}
	for _, part := range []struct{ contentType, body string }{{"text/plain; charset=utf-8", e.Request.Text}, {"text/html; charset=utf-8", e.Request.HTML}} {
		if part.body == "" {
			continue
		}
		var h messageMail.InlineHeader
		h.Set("Content-Type", part.contentType)
		body, err := inline.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(body, part.body); err != nil {
			return nil, err
		}
		if err := body.Close(); err != nil {
			return nil, err
		}
	}
	if err := inline.Close(); err != nil {
		return nil, err
	}
	for _, attachment := range e.Request.Attachments {
		var h messageMail.AttachmentHeader
		h.SetFilename(attachment.Filename)
		h.Set("Content-Type", attachment.ContentType)
		part, err := w.CreateAttachment(h)
		if err != nil {
			return nil, err
		}
		content, err := base64.StdEncoding.DecodeString(attachment.Content)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(content); err != nil {
			return nil, err
		}
		if err := part.Close(); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	block, _ := pem.Decode([]byte(d.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("signing key is unavailable")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	var signed bytes.Buffer
	err = dkim.Sign(&signed, bytes.NewReader(raw.Bytes()), &dkim.SignOptions{
		Domain: d.Name, Selector: d.Selector, Signer: key,
		HeaderCanonicalization: dkim.CanonicalizationRelaxed, BodyCanonicalization: dkim.CanonicalizationRelaxed,
		HeaderKeys: []string{"From", "To", "Cc", "Reply-To", "Subject", "Date", "Message-ID", "MIME-Version", "Content-Type"},
	})
	return signed.Bytes(), err
}
