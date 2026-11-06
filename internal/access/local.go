// Package access applies the browser and host boundary of a local-only API.
package access

import (
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Wrap rejects DNS rebinding hosts and browser origins unless explicitly allowed.
// This is not authentication against other processes owned by the same OS user.
func Wrap(next http.Handler, origins []string) (http.Handler, error) {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || strings.Contains(origin, "*") {
			return nil, fmt.Errorf("allow-origin must be an exact origin without path or wildcard: %q", origin)
		}
		switch u.Scheme {
		case "http", "https", "chrome-extension", "moz-extension":
		default:
			return nil, fmt.Errorf("unsupported origin scheme: %q", u.Scheme)
		}
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		host := r.Host
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		} else if strings.Contains(host, ":") {
			var err error
			host, _, err = net.SplitHostPort(host)
			if err != nil {
				deny(w, "invalid_host", "expected a loopback Host header")
				return
			}
		}
		if !loopbackHost(host) {
			deny(w, "invalid_host", "only localhost and loopback IP Host headers are accepted")
			return
		}
		if r.RemoteAddr != "" {
			peer, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil || net.ParseIP(peer) == nil || !net.ParseIP(peer).IsLoopback() {
				deny(w, "nonlocal_peer", "only loopback clients are accepted")
				return
			}
		}
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			if !allowed[origin] {
				deny(w, "origin_denied", "browser origin is not explicitly allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		if r.Method == http.MethodOptions {
			if origin == "" {
				deny(w, "origin_denied", "preflight requires an allowed Origin")
				return
			}
			requested := r.Header.Get("Access-Control-Request-Method")
			if requested != http.MethodPost && requested != http.MethodGet {
				deny(w, "method_denied", "unsupported preflight method")
				return
			}
			for _, h := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				if h = strings.ToLower(strings.TrimSpace(h)); h != "" && h != "content-type" {
					deny(w, "header_denied", "unsupported preflight header")
					return
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPost {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnsupportedMediaType)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "content_type", "message": "POST requests require application/json"}})
				return
			}
		}
		next.ServeHTTP(w, r)
	}), nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func deny(w http.ResponseWriter, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
