package api

import (
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// apiKeyHeader is the header the spec's api_key security scheme names.
// Some proxies (nginx by default) drop headers with underscores; the README
// notes the setting that keeps it.
const apiKeyHeader = "api_key"

// requireScope lets a request through only if its API key grants scope.
// 401 means "we don't know who you are" (no key, or an unknown key); 403
// means "we know who you are, and you may not do this". Authentication runs
// before the body is read, so unauthenticated clients cost almost nothing.
func (s *server) requireScope(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := r.Header.Get(apiKeyHeader)
		if secret == "" {
			writeError(w, http.StatusUnauthorized, errUnauthorized, "Missing api_key header")
			return
		}
		key, ok, err := s.keys.Lookup(r.Context(), secret)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, errUnauthorized, "Unknown API key")
			return
		}
		if !key.Has(scope) {
			s.logger.Info("API key lacks scope", "key", key.Name, "scope", scope)
			writeError(w, http.StatusForbidden, errForbidden, "This API key does not have the "+scope+" scope")
			return
		}
		next(w, r)
	}
}

// methodNotAllowed answers requests to a known path with the wrong method.
func methodNotAllowed(allowed ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		writeError(w, http.StatusMethodNotAllowed, errMethodNotAllowed, r.Method+" is not supported here")
	}
}

// statusRecorder remembers the status code a handler sent, for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// logRequests writes one log line per request. The API key is never logged.
func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "duration", time.Since(start).String())
	})
}

// recoverPanics turns a panic in any handler into a logged 500 response
// instead of a dropped connection.
func (s *server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler { // the standard library's signal to drop the connection
				panic(v)
			}
			s.logger.Error("panic serving request", "method", r.Method, "path", r.URL.Path,
				"panic", v, "stack", string(debug.Stack()))
			writeError(w, http.StatusInternalServerError, errInternal, "Internal server error")
		}()
		next.ServeHTTP(w, r)
	})
}
