package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/marvelousu/local-jp-translate/internal/translate"
)

func TestDocumentedChatRequestWorksWithDefaultModel(t *testing.T) {
	doc, err := os.ReadFile("../../docs/API.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(strings.ReplaceAll(string(doc), "\r\n", "\n"), "## POST /v1/chat/completions")
	if !ok {
		t.Fatal("chat API documentation missing")
	}
	_, section, ok = strings.Cut(section, "```json\n")
	if !ok {
		t.Fatal("chat request example missing")
	}
	body, _, ok := strings.Cut(section, "```")
	if !ok {
		t.Fatal("unclosed chat request example")
	}
	fake := &fakeBackend{}
	server := httptest.NewServer(New(Options{Service: translate.NewService(translate.Config{}, fake)}))
	defer server.Close()
	response, payload := postJSON(t, server.URL+"/v1/chat/completions", body)
	if response.StatusCode != http.StatusOK || fake.calls != 1 {
		t.Fatalf("documented chat request: HTTP%d, calls%d, %s", response.StatusCode, fake.calls, payload)
	}
}
