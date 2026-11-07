package sender

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"time"

	bolt "go.etcd.io/bbolt"
)

type endpoint func(http.ResponseWriter, *http.Request, identity) error

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func result(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, map[string]any{"data": data})
}
func decode(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 15*1024*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			return bad("request_too_large", "The request exceeds 15 MiB.", 413)
		}
		return bad("invalid_json", "Provide a JSON object matching the documented request fields.", 400)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return bad("invalid_json", "Provide exactly one JSON object.", 400)
	}
	return nil
}
func writeError(w http.ResponseWriter, err error) {
	var p *problem
	switch {
	case errors.As(err, &p):
	case errors.Is(err, errMissing):
		p = &problem{"not_found", "The requested resource does not exist.", 404}
	default:
		p = &problem{"internal_error", "The operation could not be completed.", 500}
	}
	writeJSON(w, p.Status, map[string]any{"error": p})
}

func (a *App) endpoint(admin bool, handler endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caller, err := a.authenticate(r)
		if err == nil && admin && !caller.Admin {
			err = bad("forbidden", "This operation requires administrator access.", 403)
		}
		if err == nil {
			a.pruneOnActivity()
			err = handler(w, r, caller)
		}
		if err != nil {
			writeError(w, err)
		}
	}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /v1/session", func(w http.ResponseWriter, r *http.Request) {
		if err := a.login(w, r); err != nil {
			writeError(w, err)
		}
	})
	mux.HandleFunc("DELETE /v1/session", a.endpoint(true, func(w http.ResponseWriter, r *http.Request, _ identity) error {
		if cookie, err := r.Cookie("sender_session"); err == nil {
			if err := a.store.db.Update(func(tx *bolt.Tx) error {
				return tx.Bucket([]byte("sessions")).Delete([]byte(a.sessionHash(cookie.Value)))
			}); err != nil {
				return err
			}
		}
		http.SetCookie(w, &http.Cookie{Name: "sender_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		result(w, 200, map[string]bool{"authenticated": false})
		return nil
	}))
	mux.HandleFunc("GET /v1/status", a.endpoint(true, a.status))
	mux.HandleFunc("GET /v1/domains", a.endpoint(true, func(w http.ResponseWriter, r *http.Request, _ identity) error {
		var domains []Domain
		err := a.store.db.View(func(tx *bolt.Tx) error { var err error; domains, err = list[Domain](tx, "domains"); return err })
		if err != nil {
			return err
		}
		for i := range domains {
			domains[i] = domains[i].public()
		}
		sort.Slice(domains, func(i, j int) bool { return domains[i].CreatedAt.After(domains[j].CreatedAt) })
		result(w, 200, domains)
		return nil
	}))
	mux.HandleFunc("POST /v1/domains", a.endpoint(true, func(w http.ResponseWriter, r *http.Request, _ identity) error {
		var input struct {
			Name string `json:"name"`
		}
		if err := decode(w, r, &input); err != nil {
			return err
		}
		d, err := a.createDomain(input.Name)
		if err != nil {
			return err
		}
		result(w, 201, d)
		return nil
	}))
	mux.HandleFunc("GET /v1/domains/{id}", a.endpoint(true, func(w http.ResponseWriter, r *http.Request, _ identity) error {
		var d Domain
		err := a.store.db.View(func(tx *bolt.Tx) error {
			var err error
			d, err = load[Domain](tx, "domains", r.PathValue("id"))
			return err
		})
		if err != nil {
			return err
		}
		result(w, 200, d.public())
		return nil
	}))
	mux.HandleFunc("POST /v1/domains/{id}/verify", a.endpoint(true, func(w http.ResponseWriter, r *http.Request, _ identity) error {
		d, err := a.verifyDomain(r.PathValue("id"))
		if err != nil {
			return err
		}
		result(w, 200, d)
		return nil
	}))
	mux.HandleFunc("DELETE /v1/domains/{id}", a.endpoint(true, func(w http.ResponseWriter, r *http.Request, _ identity) error {
		if err := a.deleteDomain(r.PathValue("id")); err != nil {
			return err
		}
		result(w, 200, map[string]bool{"deleted": true})
		return nil
	}))
	mux.HandleFunc("GET /v1/api-keys", a.endpoint(true, func(w http.ResponseWriter, r *http.Request, _ identity) error {
		var keys []APIKey
		err := a.store.db.View(func(tx *bolt.Tx) error { var err error; keys, err = list[APIKey](tx, "keys"); return err })
		if err != nil {
			return err
		}
		for i := range keys {
			keys[i] = keys[i].public()
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].CreatedAt.After(keys[j].CreatedAt) })
		result(w, 200, keys)
		return nil
	}))
	mux.HandleFunc("POST /v1/api-keys", a.endpoint(true, a.createKey))
	mux.HandleFunc("DELETE /v1/api-keys/{id}", a.endpoint(true, func(w http.ResponseWriter, r *http.Request, _ identity) error {
		err := a.store.db.Update(func(tx *bolt.Tx) error {
			if _, err := load[APIKey](tx, "keys", r.PathValue("id")); err != nil {
				return err
			}
			return tx.Bucket([]byte("keys")).Delete([]byte(r.PathValue("id")))
		})
		if err != nil {
			return err
		}
		result(w, 200, map[string]bool{"deleted": true})
		return nil
	}))
	mux.HandleFunc("POST /v1/emails", a.endpoint(false, func(w http.ResponseWriter, r *http.Request, caller identity) error {
		var input SendRequest
		if err := decode(w, r, &input); err != nil {
			return err
		}
		e, err := a.submit(input, caller, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		result(w, 202, e)
		return nil
	}))
	mux.HandleFunc("GET /v1/emails", a.endpoint(false, a.listEmails))
	mux.HandleFunc("GET /v1/emails/{id}", a.endpoint(false, func(w http.ResponseWriter, r *http.Request, caller identity) error {
		var e Email
		err := a.store.db.View(func(tx *bolt.Tx) error {
			var err error
			e, err = load[Email](tx, "emails", r.PathValue("id"))
			if err != nil {
				return err
			}
			if !caller.Admin && e.Owner != caller.Owner {
				return errMissing
			}
			return nil
		})
		if err != nil {
			return err
		}
		result(w, 200, e.public())
		return nil
	}))
	for _, action := range []string{"retry", "cancel"} {
		mux.HandleFunc("POST /v1/emails/{id}/"+action, a.endpoint(false, func(w http.ResponseWriter, r *http.Request, caller identity) error {
			e, err := a.changeEmail(r.PathValue("id"), action, caller)
			if err != nil {
				return err
			}
			result(w, 200, e)
			return nil
		}))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func (a *App) createKey(w http.ResponseWriter, r *http.Request, _ identity) error {
	var input struct {
		Name     string `json:"name"`
		DomainID string `json:"domain_id,omitempty"`
	}
	if err := decode(w, r, &input); err != nil {
		return err
	}
	if len(input.Name) < 1 || len(input.Name) > 80 {
		return bad("invalid_name", "Provide an API key name between 1 and 80 characters.", 422)
	}
	token := "sk_" + identifier("") + identifier("")
	key := APIKey{ID: identifier("key_"), Name: input.Name, Prefix: token[:11], DomainID: input.DomainID, CreatedAt: time.Now().UTC(), Hash: tokenHash(token)}
	err := a.store.db.Update(func(tx *bolt.Tx) error {
		if input.DomainID != "" {
			if _, err := load[Domain](tx, "domains", input.DomainID); err != nil {
				return bad("invalid_domain", "The selected domain does not exist.", 422)
			}
		}
		return save(tx, "keys", key.ID, key)
	})
	if err != nil {
		return err
	}
	result(w, 201, map[string]any{"key": key.public(), "token": token})
	return nil
}

func (a *App) listEmails(w http.ResponseWriter, r *http.Request, caller identity) error {
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return bad("invalid_limit", "limit must be between 1 and 100.", 422)
		}
	}
	var emails []Email
	err := a.store.db.View(func(tx *bolt.Tx) error { var err error; emails, err = list[Email](tx, "email_index"); return err })
	if err != nil {
		return err
	}
	sort.Slice(emails, func(i, j int) bool {
		if emails[i].CreatedAt.Equal(emails[j].CreatedAt) {
			return emails[i].ID > emails[j].ID
		}
		return emails[i].CreatedAt.After(emails[j].CreatedAt)
	})
	filtered := []Email{}
	after := r.URL.Query().Get("after")
	started := after == ""
	for _, e := range emails {
		if !caller.Admin && e.Owner != caller.Owner {
			continue
		}
		if !started {
			if e.ID == after {
				started = true
			}
			continue
		}
		if status := r.URL.Query().Get("status"); status != "" && e.Status != status {
			continue
		}
		filtered = append(filtered, e.public())
	}
	if !started {
		return bad("invalid_cursor", "The pagination cursor was not found.", 422)
	}
	hasMore := len(filtered) > limit
	if hasMore {
		filtered = filtered[:limit]
	}
	next := ""
	if hasMore {
		next = filtered[len(filtered)-1].ID
	}
	writeJSON(w, 200, map[string]any{"data": filtered, "has_more": hasMore, "next_cursor": next})
	return nil
}

func (a *App) status(w http.ResponseWriter, r *http.Request, _ identity) error {
	counts := map[string]int{"queued": 0, "sending": 0, "accepted": 0, "failed": 0, "partially_accepted": 0, "canceled": 0}
	var domains, keys int
	err := a.store.db.View(func(tx *bolt.Tx) error {
		emails, err := list[Email](tx, "email_index")
		if err != nil {
			return err
		}
		for _, e := range emails {
			counts[e.Status]++
		}
		domains = tx.Bucket([]byte("domains")).Stats().KeyN
		keys = tx.Bucket([]byte("keys")).Stats().KeyN
		return nil
	})
	if err != nil {
		return err
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	info, err := os.Stat(filepath.Join(a.config.DataDir, "sender.db"))
	if err != nil {
		return err
	}
	result(w, 200, map[string]any{
		"version": a.config.Version, "hostname": a.config.Hostname, "public_ip": a.config.PublicIP,
		"require_tls": a.config.RequireTLS, "envelope_from": a.config.EnvelopeFrom,
		"retention_days": int(a.config.Retention / (24 * time.Hour)), "domains": domains, "api_keys": keys,
		"emails": counts, "heap_bytes": memory.HeapAlloc, "database_bytes": info.Size(),
	})
	return nil
}
