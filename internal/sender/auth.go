package sender

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func sameToken(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(tokenHash(a)), []byte(tokenHash(b))) == 1
}

func (a *App) sessionHash(token string) string {
	return tokenHash(a.config.AdminToken + "\x00" + token)
}

func (a *App) authenticate(r *http.Request) (identity, error) {
	if value := r.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
		token := strings.TrimPrefix(value, "Bearer ")
		if sameToken(token, a.config.AdminToken) {
			return identity{Owner: "admin", Admin: true}, nil
		}
		hash := tokenHash(token)
		var caller identity
		err := a.store.db.View(func(tx *bolt.Tx) error {
			keys, err := list[APIKey](tx, "keys")
			if err != nil {
				return err
			}
			for _, key := range keys {
				if subtle.ConstantTimeCompare([]byte(key.Hash), []byte(hash)) == 1 {
					caller = identity{Owner: key.ID, DomainID: key.DomainID}
					return nil
				}
			}
			return bad("unauthorized", "The API key is invalid or has been revoked.", 401)
		})
		return caller, err
	}
	cookie, err := r.Cookie("sender_session")
	if err != nil {
		return identity{}, bad("unauthorized", "Sign in or provide a Bearer API key.", 401)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			return identity{}, bad("forbidden", "Cross-site requests are not allowed.", 403)
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				return identity{}, bad("forbidden", "The request origin does not match this installation.", 403)
			}
		}
	}
	err = a.store.db.View(func(tx *bolt.Tx) error {
		session, err := load[struct {
			Expires time.Time `json:"expires"`
		}](tx, "sessions", a.sessionHash(cookie.Value))
		if err != nil || session.Expires.Before(time.Now()) {
			return bad("unauthorized", "Your session has expired. Sign in again.", 401)
		}
		return nil
	})
	return identity{Owner: "admin", Admin: true}, err
}

func (a *App) login(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Token string `json:"token"`
	}
	if err := decode(w, r, &input); err != nil {
		return err
	}
	if !sameToken(input.Token, a.config.AdminToken) {
		return bad("unauthorized", "The administrator token is incorrect.", 401)
	}
	token := identifier("") + identifier("")
	expires := time.Now().UTC().Add(7 * 24 * time.Hour)
	err := a.store.db.Update(func(tx *bolt.Tx) error {
		return save(tx, "sessions", a.sessionHash(token), struct {
			Expires time.Time `json:"expires"`
		}{expires})
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "sender_session", Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: 7 * 24 * 3600})
	writeJSON(w, 200, map[string]bool{"authenticated": true})
	return nil
}
