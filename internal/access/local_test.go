package access

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, host, origin, peer, contentType string
		want                                  int
	}{
		{"local", "127.0.0.1:8787", "", "127.0.0.1:4321", "application/json", 200},
		{"localhost", "localhost:8787", "", "127.0.0.1:4321", "application/json; charset=utf-8", 200},
		{"ipv6", "[::1]:8787", "", "[::1]:4321", "application/json", 200},
		{"bare ipv6", "[::1]", "", "[::1]:4321", "application/json", 200},
		{"rebinding", "evil.example:8787", "", "127.0.0.1:4321", "application/json", 403},
		{"website", "127.0.0.1:8787", "https://evil.example", "127.0.0.1:4321", "application/json", 403},
		{"explicit origin", "127.0.0.1:8787", "chrome-extension://abcdef", "127.0.0.1:4321", "application/json", 200},
		{"lan peer", "127.0.0.1:8787", "", "192.168.1.4:4321", "application/json", 403},
		{"simple form", "127.0.0.1:8787", "", "127.0.0.1:4321", "text/plain", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), []string{"chrome-extension://abcdef"})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "http://127.0.0.1:8787/translate", nil)
			r.Host, r.RemoteAddr = tc.host, tc.peer
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestOriginConfigurationAndPreflight(t *testing.T) {
	for _, bad := range []string{"*", "null", "https://example.com/", "https://example.com/path", "https://user@example.com", "file://test"} {
		if _, err := Wrap(http.NotFoundHandler(), []string{bad}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	h, _ := Wrap(http.NotFoundHandler(), []string{"https://example.com"})
	r := httptest.NewRequest("OPTIONS", "http://127.0.0.1:8787/translate", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("bad preflight: %d", w.Code)
	}
	r.Header.Set("Access-Control-Request-Headers", "x-arbitrary")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("arbitrary preflight headers allowed")
	}
}
