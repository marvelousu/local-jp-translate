package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type translateCallRequest struct {
	Text      string         `json:"text"`
	Source    string         `json:"source,omitempty"`
	Target    string         `json:"target,omitempty"`
	Style     string         `json:"style,omitempty"`
	Glossary  []glossaryCall `json:"glossary,omitempty"`
	Context   string         `json:"context,omitempty"`
	MaxTokens int            `json:"max_tokens,omitempty"`
}

type glossaryCall struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type translateCallResponse struct {
	TranslatedText string `json:"translated_text"`
	Model          string `json:"model"`
	DurationMS     int64  `json:"duration_ms"`
}

type apiErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func runTranslate(args []string) error {
	fs := flag.NewFlagSet("translate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	url := fs.String("url", "http://127.0.0.1:8787", "jptranslate server base URL")
	textFlag := fs.String("text", "", "text to translate (otherwise use positional text or stdin)")
	source := fs.String("source", "auto", "source language code or auto")
	target := fs.String("target", "ja", "target language code")
	style := fs.String("style", "default", "polite, plain, or default")
	contextText := fs.String("context", "", "reference context")
	maxTokens := fs.Int("max-tokens", 0, "generation limit (0 uses the server default)")
	timeout := fs.Duration("timeout", 180*time.Second, "request timeout")
	asJSON := fs.Bool("json", false, "print the full JSON response")
	var glossary stringList
	fs.Var(&glossary, "glossary", "glossary pair as 'source => target' (repeatable)")

	// The standard flag package stops at the first non-flag token. Pull leading
	// positional text out first so `jptranslate translate "text" --target ja`
	// works as documented.
	var positional []string
	rest := args
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		positional = append(positional, rest[0])
		rest = rest[1:]
	}

	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	positional = append(positional, fs.Args()...)

	text := *textFlag
	if len(positional) > 0 {
		if text != "" {
			return errors.New("provide text either positionally or with --text, not both")
		}
		text = strings.Join(positional, " ")
	}
	if text == "" {
		info, err := os.Stdin.Stat()
		if err == nil && info.Mode()&os.ModeCharDevice == 0 {
			data, readErr := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
			if readErr != nil {
				return fmt.Errorf("read stdin: %w", readErr)
			}
			text = string(data)
		}
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("text is required (argument, --text, or stdin)")
	}

	pairs := make([]glossaryCall, 0, len(glossary))
	for _, raw := range glossary {
		parts := strings.SplitN(raw, "=>", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return fmt.Errorf("invalid --glossary %q: use 'source => target'", raw)
		}
		pairs = append(pairs, glossaryCall{Source: strings.TrimSpace(parts[0]), Target: strings.TrimSpace(parts[1])})
	}

	payload, err := json.Marshal(translateCallRequest{
		Text:      text,
		Source:    *source,
		Target:    *target,
		Style:     *style,
		Glossary:  pairs,
		Context:   *contextText,
		MaxTokens: *maxTokens,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	endpoint := strings.TrimRight(*url, "/") + "/translate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Transport: &http.Transport{Proxy: nil},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var envelope apiErrorEnvelope
		if json.Unmarshal(body, &envelope) == nil && envelope.Error.Code != "" {
			return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded translateCallResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if decoded.TranslatedText == "" {
		return errors.New("server returned an empty translation")
	}
	if *asJSON {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err != nil {
			return err
		}
		fmt.Println(pretty.String())
		return nil
	}
	fmt.Println(decoded.TranslatedText)
	return nil
}
