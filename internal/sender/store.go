package sender

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var buckets = []string{"domains", "emails", "email_index", "keys", "idempotency", "sessions", "queue"}

type Store struct{ db *bolt.DB }

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(filepath.Join(dir, "sender.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open data store: %w", err)
	}
	if err := os.Chmod(filepath.Join(dir, "sender.db"), 0600); err != nil {
		db.Close()
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

func load[T any](tx *bolt.Tx, bucket, key string) (T, error) {
	var value T
	raw := tx.Bucket([]byte(bucket)).Get([]byte(key))
	if raw == nil {
		return value, errMissing
	}
	err := json.Unmarshal(raw, &value)
	return value, err
}
func save(tx *bolt.Tx, bucket, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return tx.Bucket([]byte(bucket)).Put([]byte(key), raw)
}
func list[T any](tx *bolt.Tx, bucket string) ([]T, error) {
	out := []T{}
	err := tx.Bucket([]byte(bucket)).ForEach(func(_, raw []byte) error {
		var value T
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		out = append(out, value)
		return nil
	})
	return out, err
}

func queueKey(t time.Time, id string, recipient int) []byte {
	return []byte(fmt.Sprintf("%020d:%s:%06d", t.UnixMilli(), id, recipient))
}
func updateEmail(tx *bolt.Tx, e *Email) error {
	q := tx.Bucket([]byte("queue"))
	old, err := load[Email](tx, "emails", e.ID)
	if err != nil && err != errMissing {
		return err
	}
	for i, r := range old.Recipients {
		if r.Status == "queued" {
			if err := q.Delete(queueKey(r.NextAttempt, old.ID, i)); err != nil {
				return err
			}
		}
	}
	e.summarize()
	if err := save(tx, "emails", e.ID, e); err != nil {
		return err
	}
	summary := *e
	summary.Request.Text = ""
	summary.Request.HTML = ""
	summary.Request.Attachments = nil
	if err := save(tx, "email_index", e.ID, summary); err != nil {
		return err
	}
	for i, r := range e.Recipients {
		if r.Status == "queued" {
			if err := q.Put(queueKey(r.NextAttempt, e.ID, i), []byte(e.ID)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Startup reconstructs the queue from durable recipient state. A connection
// interrupted after remote acceptance may be retried with the same Message-ID.
func (s *Store) recover() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket([]byte("queue")); err != nil {
			return err
		}
		if _, err := tx.CreateBucket([]byte("queue")); err != nil {
			return err
		}
		emails, err := list[Email](tx, "email_index")
		if err != nil {
			return err
		}
		for _, summary := range emails {
			email, err := load[Email](tx, "emails", summary.ID)
			if err != nil {
				return err
			}
			for i := range email.Recipients {
				r := &email.Recipients[i]
				if r.Status == "sending" {
					r.Status = "queued"
					r.NextAttempt = time.Now().UTC()
					r.Attempts = append(r.Attempts, Attempt{time.Now().UTC(), "interrupted", "Connection ended before its outcome was recorded; retry may duplicate delivery."})
				}
			}
			if err := updateEmail(tx, &email); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) prune(retention time.Duration) error {
	cutoff := time.Now().Add(-retention)
	return s.db.Update(func(tx *bolt.Tx) error {
		emails, err := list[Email](tx, "email_index")
		if err != nil {
			return err
		}
		for _, e := range emails {
			if e.Status != "queued" && e.Status != "sending" && e.UpdatedAt.Before(cutoff) {
				if err := tx.Bucket([]byte("emails")).Delete([]byte(e.ID)); err != nil {
					return err
				}
				if err := tx.Bucket([]byte("email_index")).Delete([]byte(e.ID)); err != nil {
					return err
				}
			}
		}
		for _, bucket := range []string{"sessions", "idempotency"} {
			c := tx.Bucket([]byte(bucket)).Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var entry struct {
					Expires time.Time `json:"expires"`
				}
				if err := json.Unmarshal(v, &entry); err != nil {
					return err
				}
				if entry.Expires.Before(time.Now()) {
					if err := c.Delete(); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}
