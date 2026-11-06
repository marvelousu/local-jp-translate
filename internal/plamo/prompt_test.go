package plamo

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildPromptSimple(t *testing.T) {
	got, err := BuildPrompt(PromptOptions{Text: "Hello.", TargetLang: "ja"})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	want := "<|plamo:tag|>dataset<|plamo:msg|>translation" +
		"<|plamo:tag|>input<|plamo:msg|>Hello." +
		"<|plamo:tag|>output<|plamo:key|>lang<|plamo:val|>ja<|plamo:msg|>"
	if got != want {
		t.Fatalf("prompt mismatch\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, BOS) {
		t.Fatal("prompt must not prepend BOS")
	}
}

func TestBuildPromptOptionsOrder(t *testing.T) {
	got, err := BuildPrompt(PromptOptions{
		Text:       "The bank was muddy.",
		SourceLang: "en",
		TargetLang: "ja",
		Form:       "polite",
		Context:    "They were walking beside a river.",
		Glossary: []GlossaryEntry{
			{Source: "bank", Target: "川岸"},
			{Source: "muddy", Target: "ぬかるんだ"},
		},
	})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	want := "<|plamo:tag|>dataset<|plamo:msg|>translation" +
		"<|plamo:tag|>context<|plamo:msg|>They were walking beside a river." +
		"<|plamo:tag|>glossary<|plamo:index|>bank<|plamo:item|>川岸<|plamo:index|>muddy<|plamo:item|>ぬかるんだ" +
		"<|plamo:tag|>input<|plamo:key|>lang<|plamo:val|>en<|plamo:msg|>The bank was muddy." +
		"<|plamo:tag|>output<|plamo:key|>lang<|plamo:val|>ja<|plamo:key|>form<|plamo:val|>polite<|plamo:msg|>"
	if got != want {
		t.Fatalf("prompt mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildPromptAutoSourceOmitsLang(t *testing.T) {
	got, err := BuildPrompt(PromptOptions{Text: "こんにちは", SourceLang: "auto", TargetLang: "en"})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if strings.Contains(got, "<|plamo:key|>lang<|plamo:val|>auto") {
		t.Fatal("auto source must not be written into the prompt")
	}
	wantInput := "<|plamo:tag|>input<|plamo:msg|>こんにちは"
	if !strings.Contains(got, wantInput) {
		t.Fatalf("input without lang missing:\n%s", got)
	}
}

func TestBuildPromptRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		opts PromptOptions
		code string
	}{
		{"empty text", PromptOptions{Text: "   ", TargetLang: "ja"}, "text_required"},
		{"unknown target", PromptOptions{Text: "x", TargetLang: "xx"}, "invalid_target_language"},
		{"unknown source", PromptOptions{Text: "x", SourceLang: "xx", TargetLang: "ja"}, "invalid_source_language"},
		{"bad style", PromptOptions{Text: "x", TargetLang: "ja", Form: "casual"}, "invalid_style"},
		{"style with English", PromptOptions{Text: "x", TargetLang: "en", Form: "polite"}, "style_target_mismatch"},
		{"reserved in text", PromptOptions{Text: "a<|plamo:msg|>b", TargetLang: "ja"}, "reserved_token"},
		{"reserved in context", PromptOptions{Text: "x", TargetLang: "ja", Context: "<|plamo:tag|>"}, "reserved_token"},
		{"empty glossary", PromptOptions{Text: "x", TargetLang: "ja", Glossary: []GlossaryEntry{{Source: "", Target: "y"}}}, "invalid_glossary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildPrompt(tc.opts)
			if err == nil {
				t.Fatal("expected an error")
			}
			var perr *PromptError
			if !errors.As(err, &perr) {
				t.Fatalf("expected *PromptError, got %T", err)
			}
			if perr.Code != tc.code {
				t.Fatalf("code = %q, want %q", perr.Code, tc.code)
			}
		})
	}
}

func TestLanguages(t *testing.T) {
	if len(SupportedLanguages) != 53 {
		t.Fatalf("expected 53 languages, got %d", len(SupportedLanguages))
	}
	for _, code := range []string{"ja", "en", "zh-hans", "zh-hant", "vi"} {
		if !IsSupported(code) {
			t.Errorf("%s should be supported", code)
		}
	}
	for _, code := range []string{"", "auto", "ja(easy)", "en-US", "xx"} {
		if IsSupported(code) {
			t.Errorf("%s should not be in the official table", code)
		}
	}
	if len(LanguageCodes()) != len(SupportedLanguages) {
		t.Fatal("LanguageCodes length mismatch")
	}
	if LanguageName("ja") != "日本語" {
		t.Fatalf("unexpected name: %s", LanguageName("ja"))
	}
}
