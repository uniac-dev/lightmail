package sender

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"sort"
	"strings"
	"time"
)

type deliveryError struct {
	detail    string
	temporary bool
}

func (e *deliveryError) Error() string { return e.detail }

func classifyDelivery(err error) error {
	var response *textproto.Error
	if errors.As(err, &response) {
		return &deliveryError{fmt.Sprintf("SMTP %d: %s", response.Code, response.Msg), response.Code < 500}
	}
	return &deliveryError{err.Error(), true}
}

type SMTPDelivery struct {
	config Config
	dns    DNS
	dial   func(context.Context, string, string) (net.Conn, error)
}

func newSMTP(config Config, dns DNS) *SMTPDelivery {
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	return &SMTPDelivery{config, dns, dialer.DialContext}
}

func publicAddress(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4"} {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func (s *SMTPDelivery) Deliver(ctx context.Context, e Email, d Domain, recipient Recipient) error {
	raw, err := buildMessage(e, d)
	if err != nil {
		return &deliveryError{"Message signing failed.", false}
	}
	domain := recipient.Address[strings.LastIndexByte(recipient.Address, '@')+1:]
	hosts, err := s.dns.LookupMX(domain)
	if err != nil {
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
			return &deliveryError{"Recipient MX lookup failed.", true}
		}
		// A domain without MX records has an implicit MX at its own A/AAAA.
		// LookupMX reports both NODATA and NXDOMAIN as IsNotFound; the address
		// lookup below establishes whether that fallback can receive mail.
		hosts = nil
	}
	if len(hosts) == 1 && hosts[0].Host == "." {
		return &deliveryError{"The recipient domain does not accept email (Null MX).", false}
	}
	if len(hosts) == 0 {
		hosts = []*net.MX{{Host: domain, Pref: 0}}
	}
	sort.SliceStable(hosts, func(i, j int) bool { return hosts[i].Pref < hosts[j].Pref })
	var last error = &deliveryError{"No public recipient mail server is reachable.", true}
	if len(hosts) > 10 {
		hosts = hosts[:10]
	}
	for _, mx := range hosts {
		host := strings.TrimSuffix(mx.Host, ".")
		addresses, err := s.dns.LookupIPAddr(host)
		if err != nil {
			last = &deliveryError{"Recipient mail server address lookup failed.", true}
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				last = &deliveryError{"Recipient mail server has no address records.", false}
			}
			continue
		}
		if len(addresses) > 8 {
			addresses = addresses[:8]
		}
		for _, remote := range addresses {
			if !publicAddress(remote.IP) {
				last = &deliveryError{"Recipient MX resolves to a non-public IP address.", false}
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			err = s.transaction(ctx, host, remote.IP.String(), e, recipient.Address, raw)
			if err == nil {
				return nil
			}
			last = err
			if failure, ok := err.(*deliveryError); ok && !failure.temporary {
				return err
			}
		}
	}
	return last
}

func (s *SMTPDelivery) transaction(ctx context.Context, host, ip string, e Email, recipient string, raw []byte) error {
	conn, err := s.dial(ctx, "tcp", net.JoinHostPort(ip, "25"))
	if err != nil {
		return &deliveryError{"Could not connect to recipient SMTP server on destination port 25.", true}
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err := conn.SetDeadline(time.Now().Add(60 * time.Second)); err != nil {
		return classifyDelivery(err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return classifyDelivery(err)
	}
	defer client.Close()
	if err := client.Hello(s.config.Hostname); err != nil {
		return classifyDelivery(err)
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return &deliveryError{"Recipient STARTTLS handshake or certificate validation failed.", true}
		}
	} else if s.config.RequireTLS {
		return &deliveryError{"Recipient mail server did not offer STARTTLS.", true}
	}
	from := s.config.EnvelopeFrom
	if from == "" {
		parsed, err := mail.ParseAddress(e.Request.From)
		if err != nil {
			return &deliveryError{"The sender address is invalid.", false}
		}
		from = parsed.Address
	}
	if err := client.Mail(from); err != nil {
		return classifyDelivery(err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return classifyDelivery(err)
	}
	writer, err := client.Data()
	if err != nil {
		return classifyDelivery(err)
	}
	if _, err := writer.Write(raw); err != nil {
		return &deliveryError{"SMTP connection ended during DATA; retry may duplicate delivery.", true}
	}
	if err := writer.Close(); err != nil {
		var response *textproto.Error
		if errors.As(err, &response) {
			return classifyDelivery(err)
		}
		return &deliveryError{"SMTP connection ended before DATA acceptance was confirmed; retry may duplicate delivery.", true}
	}
	// DATA acceptance is the delivery outcome; a failed QUIT does not undo it.
	_ = client.Quit()
	return nil
}
