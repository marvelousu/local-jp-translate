// Package plamo builds PLaMo 3 Translate PFML3 prompts and describes the
// languages supported by the pinned official model card.
package plamo

import "strings"

// PFML3 control markers used by the official prompt format.
const (
	Tag      = "<|plamo:tag|>"
	Msg      = "<|plamo:msg|>"
	Key      = "<|plamo:key|>"
	Val      = "<|plamo:val|>"
	Index    = "<|plamo:index|>"
	Item     = "<|plamo:item|>"
	BOS      = "<|plamo:bos|>"
	Reserved = "<|plamo:"
)

// StopMarkers are the stop sequences recommended by the official model card
// for /v1/completions.
var StopMarkers = []string{Tag, BOS}

// GlossaryEntry is one source-to-target terminology pair.
type GlossaryEntry struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// PromptOptions holds the fields accepted by BuildPrompt. Text is the source
// text. SourceLang may be empty or "auto" to let the model detect it.
// TargetLang is required. Form must be empty, "plain", or "polite" and is
// only valid when TargetLang is "ja".
type PromptOptions struct {
	Text       string
	SourceLang string
	TargetLang string
	Form       string
	Context    string
	Glossary   []GlossaryEntry
}

// BuildPrompt renders a PFML3 /v1/completions prompt for translation.
//
// The prompt starts with the "dataset/translation" element and ends with an
// empty "output" message. No BOS token is prepended: llama.cpp adds it and the
// official model card explicitly requires callers not to add it manually.
func BuildPrompt(opts PromptOptions) (string, error) {
	if strings.TrimSpace(opts.Text) == "" {
		return "", &PromptError{Code: "text_required", Message: "text must not be empty"}
	}
	if opts.TargetLang == "" {
		return "", &PromptError{Code: "invalid_target_language", Message: "target language is required"}
	}
	if !IsSupported(opts.TargetLang) {
		return "", &PromptError{Code: "invalid_target_language", Message: "unsupported target language: " + opts.TargetLang}
	}
	source := opts.SourceLang
	if source == "auto" {
		source = ""
	}
	if source != "" && !IsSupported(source) {
		return "", &PromptError{Code: "invalid_source_language", Message: "unsupported source language: " + opts.SourceLang}
	}
	if opts.Form != "" && opts.Form != "plain" && opts.Form != "polite" {
		return "", &PromptError{Code: "invalid_style", Message: "style must be polite, plain, or default"}
	}
	if opts.Form != "" && opts.TargetLang != "ja" {
		return "", &PromptError{Code: "style_target_mismatch", Message: "style is only supported when target is ja"}
	}

	type field struct {
		name  string
		value string
	}
	fields := []field{
		{"text", opts.Text},
		{"context", opts.Context},
	}
	for _, entry := range opts.Glossary {
		fields = append(fields,
			field{"glossary source", entry.Source},
			field{"glossary target", entry.Target},
		)
	}
	for _, field := range fields {
		if strings.Contains(field.value, Reserved) {
			return "", &PromptError{
				Code:    "reserved_token",
				Message: field.name + " must not contain reserved PFML markers (" + Reserved + ")",
			}
		}
	}
	for _, entry := range opts.Glossary {
		if strings.TrimSpace(entry.Source) == "" || strings.TrimSpace(entry.Target) == "" {
			return "", &PromptError{Code: "invalid_glossary", Message: "glossary entries need non-empty source and target"}
		}
	}

	var b strings.Builder
	b.WriteString(Tag)
	b.WriteString("dataset")
	b.WriteString(Msg)
	b.WriteString("translation")

	if opts.Context != "" {
		b.WriteString(Tag)
		b.WriteString("context")
		b.WriteString(Msg)
		b.WriteString(opts.Context)
	}

	if len(opts.Glossary) > 0 {
		b.WriteString(Tag)
		b.WriteString("glossary")
		for _, entry := range opts.Glossary {
			b.WriteString(Index)
			b.WriteString(entry.Source)
			b.WriteString(Item)
			b.WriteString(entry.Target)
		}
	}

	b.WriteString(Tag)
	b.WriteString("input")
	if source != "" {
		b.WriteString(Key)
		b.WriteString("lang")
		b.WriteString(Val)
		b.WriteString(source)
	}
	b.WriteString(Msg)
	b.WriteString(opts.Text)

	b.WriteString(Tag)
	b.WriteString("output")
	b.WriteString(Key)
	b.WriteString("lang")
	b.WriteString(Val)
	b.WriteString(opts.TargetLang)
	if opts.Form != "" {
		b.WriteString(Key)
		b.WriteString("form")
		b.WriteString(Val)
		b.WriteString(opts.Form)
	}
	b.WriteString(Msg)

	return b.String(), nil
}

// PromptError describes an invalid prompt field.
type PromptError struct {
	Code    string
	Message string
}

func (e *PromptError) Error() string { return e.Message }
