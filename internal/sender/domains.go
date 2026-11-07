package sender

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

type networkDNS struct{}

func (networkDNS) LookupTXT(name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupTXT(ctx, name)
}
func (networkDNS) LookupMX(name string) ([]*net.MX, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupMX(ctx, name)
}
func (networkDNS) LookupIPAddr(name string) ([]net.IPAddr, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupIPAddr(ctx, name)
}

func domainName(value string) (string, error) {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if len(name) > 253 || !strings.Contains(name, ".") || net.ParseIP(name) != nil {
		return "", bad("invalid_domain", "Enter a domain such as example.com.", 422)
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", bad("invalid_domain", "The domain name is invalid.", 422)
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", bad("invalid_domain", "Use an ASCII or punycode domain name.", 422)
			}
		}
	}
	return name, nil
}

func (a *App) createDomain(value string) (Domain, error) {
	name, err := domainName(value)
	if err != nil {
		return Domain{}, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return Domain{}, err
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return Domain{}, err
	}
	selector := identifier("sender-")[:15]
	d := Domain{
		ID: identifier("dom_"), Name: name, Selector: selector, Status: "pending",
		CreatedAt:  time.Now().UTC(),
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})),
		Records:    []DNSRecord{{"TXT", selector + "._domainkey." + name, "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(pub), true, "pending"}},
	}
	if a.config.PublicIP != "" {
		version := "ip4:"
		if net.ParseIP(a.config.PublicIP).To4() == nil {
			version = "ip6:"
		}
		d.Records = append(d.Records, DNSRecord{"TXT", name, "v=spf1 " + version + a.config.PublicIP + " ~all", false, "pending"})
	}
	d.Records = append(d.Records, DNSRecord{"TXT", "_dmarc." + name, "v=DMARC1; p=none", false, "pending"})
	err = a.store.db.Update(func(tx *bolt.Tx) error {
		domains, err := list[Domain](tx, "domains")
		if err != nil {
			return err
		}
		for _, existing := range domains {
			if existing.Name == name {
				return bad("domain_exists", "This domain has already been added.", 409)
			}
		}
		return save(tx, "domains", d.ID, d)
	})
	return d.public(), err
}

func dnsTag(record, name string) string {
	found := ""
	for _, tag := range strings.Split(record, ";") {
		parts := strings.SplitN(strings.TrimSpace(tag), "=", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], name) {
			if found != "" {
				return ""
			}
			found = strings.Join(strings.Fields(parts[1]), "")
		}
	}
	return found
}

func (a *App) verifyDomain(id string) (Domain, error) {
	var d Domain
	err := a.store.db.View(func(tx *bolt.Tx) error { var err error; d, err = load[Domain](tx, "domains", id); return err })
	if err != nil {
		return Domain{}, err
	}
	for i := range d.Records {
		record := &d.Records[i]
		values, lookupErr := a.dns.LookupTXT(record.Name)
		record.Status = "pending"
		if lookupErr != nil {
			if e, ok := lookupErr.(*net.DNSError); ok && e.IsNotFound {
				continue
			}
			return Domain{}, bad("dns_unavailable", "DNS could not be queried. Try verification again.", 503)
		}
		if record.Required {
			if len(values) == 1 && dnsTag(values[0], "p") == dnsTag(record.Value, "p") &&
				(dnsTag(values[0], "k") == "" || strings.EqualFold(dnsTag(values[0], "k"), "rsa")) &&
				(dnsTag(values[0], "v") == "" || strings.EqualFold(dnsTag(values[0], "v"), "DKIM1")) &&
				!strings.Contains(dnsTag(values[0], "t"), "y") {
				record.Status = "verified"
			}
		} else {
			for _, value := range values {
				if strings.HasPrefix(record.Value, "v=spf1 ") && strings.HasPrefix(value, "v=spf1 ") ||
					strings.HasPrefix(record.Value, "v=DMARC1;") && strings.HasPrefix(value, "v=DMARC1;") {
					record.Status = "present"
				}
			}
		}
	}
	d.Status = "pending"
	d.VerifiedAt = nil
	if d.Records[0].Status == "verified" {
		now := time.Now().UTC()
		d.Status = "verified"
		d.VerifiedAt = &now
	}
	err = a.store.db.Update(func(tx *bolt.Tx) error {
		if _, err := load[Domain](tx, "domains", id); err != nil {
			return err
		}
		return save(tx, "domains", id, d)
	})
	return d.public(), err
}

func (a *App) deleteDomain(id string) error {
	return a.store.db.Update(func(tx *bolt.Tx) error {
		if _, err := load[Domain](tx, "domains", id); err != nil {
			return err
		}
		emails, err := list[Email](tx, "email_index")
		if err != nil {
			return err
		}
		for _, e := range emails {
			if e.DomainID == id && (e.Status == "queued" || e.Status == "sending") {
				return bad("domain_in_use", "Cancel or finish this domain's queued messages before removing it.", 409)
			}
		}
		return tx.Bucket([]byte("domains")).Delete([]byte(id))
	})
}
