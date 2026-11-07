package sender

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

type Deliverer interface {
	Deliver(context.Context, Email, Domain, Recipient) error
}

type work struct {
	email     Email
	domain    Domain
	recipient int
}

func (a *App) wake() {
	select {
	case a.notify <- struct{}{}:
	default:
	}
}

func (a *App) claim() (*work, time.Duration, error) {
	var claimed *work
	var delay time.Duration = -1
	err := a.store.db.Update(func(tx *bolt.Tx) error {
		q := tx.Bucket([]byte("queue"))
		c := q.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			parts := strings.Split(string(k), ":")
			if len(parts) != 3 {
				return fmt.Errorf("invalid queue entry")
			}
			at, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil {
				return err
			}
			if wait := time.Until(time.UnixMilli(at)); wait > 0 {
				delay = wait
				return nil
			}
			e, err := load[Email](tx, "emails", parts[1])
			if err == errMissing {
				if err := c.Delete(); err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			i, err := strconv.Atoi(parts[2])
			if err != nil || i >= len(e.Recipients) {
				return fmt.Errorf("invalid queue recipient")
			}
			r := &e.Recipients[i]
			if r.Status != "queued" || r.NextAttempt.UnixMilli() != at {
				if err := c.Delete(); err != nil {
					return err
				}
				continue
			}
			d, err := load[Domain](tx, "domains", e.DomainID)
			if err != nil {
				return err
			}
			r.Status = "sending"
			if err := updateEmail(tx, &e); err != nil {
				return err
			}
			claimed = &work{e, d, i}
			return nil
		}
		return nil
	})
	return claimed, delay, err
}

var retryBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 4 * time.Hour, 12 * time.Hour}

func (a *App) finish(w *work, deliveryErr error) error {
	return a.store.db.Update(func(tx *bolt.Tx) error {
		e, err := load[Email](tx, "emails", w.email.ID)
		if err != nil {
			return err
		}
		r := &e.Recipients[w.recipient]
		now := time.Now().UTC()
		r.Status = "accepted"
		r.LastError = ""
		if deliveryErr != nil {
			r.LastError = deliveryErr.Error()
			if len(r.LastError) > 512 {
				r.LastError = r.LastError[:512]
			}
			r.Status = "failed"
			temporary := true
			if err, ok := deliveryErr.(*deliveryError); ok {
				temporary = err.temporary
			}
			if temporary && len(r.Attempts) < len(retryBackoff) && time.Since(e.CreatedAt) < 48*time.Hour {
				r.Status = "queued"
				r.NextAttempt = now.Add(retryBackoff[len(r.Attempts)])
			}
		}
		r.Attempts = append(r.Attempts, Attempt{now, r.Status, r.LastError})
		return updateEmail(tx, &e)
	})
}

func (a *App) RunWorker(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		w, delay, err := a.claim()
		if err != nil {
			return err
		}
		if w != nil {
			deliveryCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			deliveryErr := a.deliverer.Deliver(deliveryCtx, w.email, w.domain, w.email.Recipients[w.recipient])
			cancel()
			if ctx.Err() != nil {
				return nil
			}
			if err := a.finish(w, deliveryErr); err != nil {
				return err
			}
			continue
		}
		var timer *time.Timer
		var next <-chan time.Time
		if delay >= 0 {
			timer = time.NewTimer(delay)
			next = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return nil
		case <-a.notify:
		case <-next:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (a *App) changeEmail(id, action string, caller identity) (Email, error) {
	var e Email
	err := a.store.db.Update(func(tx *bolt.Tx) error {
		var err error
		e, err = load[Email](tx, "emails", id)
		if err != nil {
			return err
		}
		if !caller.Admin && e.Owner != caller.Owner {
			return errMissing
		}
		changed := false
		for i := range e.Recipients {
			r := &e.Recipients[i]
			if action == "cancel" && r.Status == "queued" {
				r.Status = "canceled"
				changed = true
			}
			if action == "retry" && r.Status == "failed" {
				r.Status = "queued"
				r.NextAttempt = time.Now().UTC()
				r.LastError = ""
				changed = true
			}
		}
		if !changed {
			return bad("invalid_state", "No recipients can be "+action+"ed in this message's current state.", 409)
		}
		return updateEmail(tx, &e)
	})
	if err == nil {
		a.wake()
	}
	return e.public(), err
}
