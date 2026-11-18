// Package auth checks the API keys that client applications send.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"strings"
)

// ScopeCreateOrder allows placing orders. It is the scope the spec's
// api_key security scheme names for POST /order.
const ScopeCreateOrder = "create_order"

// Key is a client application's API key, as configured.
type Key struct {
	Name   string // who the key belongs to; safe to log, unlike the key itself
	scopes map[string]bool
}

// Has reports whether the key grants scope.
func (k Key) Has(scope string) bool { return k.scopes[scope] }

// KeyStore looks up API keys. It is an interface so keys can move from
// configuration to a database or an auth service without touching the
// HTTP layer.
type KeyStore interface {
	// Lookup returns the key for secret, and false if no key matches.
	Lookup(ctx context.Context, secret string) (Key, bool, error)
}

// StaticKeys is a KeyStore built from configuration.
//
// It keeps only a SHA-256 hash of each secret and compares hashes in
// constant time, so neither the secret's contents nor its length can be
// guessed from how long a lookup takes. Several keys may grant the same
// scopes, which allows rotation: add the new key, move clients, remove the
// old one.
type StaticKeys struct {
	keys []hashedKey
}

var _ KeyStore = (*StaticKeys)(nil) // compile-time check that StaticKeys is a KeyStore

type hashedKey struct {
	hash [sha256.Size]byte
	key  Key
}

// ParseKeys reads keys from a configuration string such as
//
//	"apitest:create_order,readonly:"
//
// Keys are separated by commas; each is "secret:scopes", with scopes
// separated by "+" and possibly empty. Keys are named key-1, key-2 and so on
// in the order given, for use in logs.
func ParseKeys(config string) (*StaticKeys, error) {
	s := &StaticKeys{}
	if strings.TrimSpace(config) == "" {
		return s, nil
	}
	for i, entry := range strings.Split(config, ",") {
		secret, scopeList, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || secret == "" {
			return nil, fmt.Errorf("API key %d: want \"secret:scopes\"", i+1)
		}
		key := Key{Name: fmt.Sprintf("key-%d", i+1), scopes: map[string]bool{}}
		for _, scope := range strings.Split(scopeList, "+") {
			if scope = strings.TrimSpace(scope); scope != "" {
				key.scopes[scope] = true
			}
		}
		hash := sha256.Sum256([]byte(secret))
		for _, existing := range s.keys {
			if existing.hash == hash {
				return nil, fmt.Errorf("API key %d is the same as %s", i+1, existing.key.Name)
			}
		}
		s.keys = append(s.keys, hashedKey{hash: hash, key: key})
	}
	return s, nil
}

// Len returns how many keys are configured.
func (s *StaticKeys) Len() int { return len(s.keys) }

// Lookup checks every configured key, without stopping at a match, so the
// time taken does not reveal which key matched.
func (s *StaticKeys) Lookup(_ context.Context, secret string) (Key, bool, error) {
	hash := sha256.Sum256([]byte(secret))
	var found Key
	ok := false
	for _, k := range s.keys {
		if subtle.ConstantTimeCompare(hash[:], k.hash[:]) == 1 {
			found, ok = k.key, true
		}
	}
	return found, ok, nil
}
