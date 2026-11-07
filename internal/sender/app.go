package sender

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type App struct {
	config    Config
	store     *Store
	dns       DNS
	deliverer Deliverer
	notify    chan struct{}
	pruneMu   sync.Mutex
	lastPrune time.Time
}

func New(config Config) (*App, error) { return newApp(config, networkDNS{}, nil) }

func newApp(config Config, dns DNS, deliverer Deliverer) (*App, error) {
	if len(config.AdminToken) < 32 {
		return nil, errors.New("SENDER_ADMIN_TOKEN must contain at least 32 characters")
	}
	hostname, err := domainName(config.Hostname)
	if err != nil {
		return nil, errors.New("SENDER_HOSTNAME must be a valid fully qualified hostname")
	}
	config.Hostname = hostname
	if config.PublicIP != "" && net.ParseIP(config.PublicIP) == nil {
		return nil, errors.New("SENDER_PUBLIC_IP must be an IPv4 or IPv6 address")
	}
	if config.EnvelopeFrom != "" {
		parsed, err := address(config.EnvelopeFrom)
		if err != nil {
			return nil, errors.New("SENDER_ENVELOPE_FROM must be a valid email address")
		}
		config.EnvelopeFrom = parsed.Address
	}
	if config.Retention <= 0 {
		config.Retention = 30 * 24 * time.Hour
	}
	store, err := OpenStore(config.DataDir)
	if err != nil {
		return nil, err
	}
	if deliverer == nil {
		deliverer = newSMTP(config, dns)
	}
	a := &App{config: config, store: store, dns: dns, deliverer: deliverer, notify: make(chan struct{}, 1)}
	if err := store.recover(); err != nil {
		store.Close()
		return nil, fmt.Errorf("recover queue: %w", err)
	}
	if err := store.prune(config.Retention); err != nil {
		store.Close()
		return nil, err
	}
	a.lastPrune = time.Now()
	return a, nil
}
func (a *App) Close() error { return a.store.Close() }

func (a *App) pruneOnActivity() {
	a.pruneMu.Lock()
	defer a.pruneMu.Unlock()
	if time.Since(a.lastPrune) < time.Hour {
		return
	}
	if err := a.store.prune(a.config.Retention); err == nil {
		a.lastPrune = time.Now()
	}
}
