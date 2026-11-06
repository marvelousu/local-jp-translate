package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marvelousu/local-jp-translate/internal/modelstore"
)

const (
	modelLicenseJA = "https://plamo.preferredai.jp/info/plamo-community-license-ja"
	modelLicenseEN = "https://plamo.preferredai.jp/info/plamo-community-license-en"
)

func runModel(args []string) error {
	if len(args) == 0 {
		return errors.New("model subcommand required: info, verify, or download")
	}
	switch args[0] {
	case "info":
		return runModelInfo(args[1:])
	case "verify":
		return runModelVerify(args[1:])
	case "download":
		return runModelDownload(args[1:])
	default:
		return fmt.Errorf("unknown model subcommand %q", args[0])
	}
}

func runModelInfo(args []string) error {
	fs := flag.NewFlagSet("model info", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *asJSON {
		payload := map[string]any{
			"built_with":           "PLaMo 3 Translate",
			"repo":                 modelstore.Repository,
			"revision":             modelstore.Revision,
			"file":                 modelstore.Filename,
			"size_bytes":           modelstore.Size,
			"sha256":               modelstore.SHA256,
			"download_url":         modelstore.URL,
			"license":              "PLaMo Community License",
			"license_url":          modelstore.LicenseURL,
			"license_url_ja":       modelLicenseJA,
			"license_url_en":       modelLicenseEN,
			"source_license":       "MIT",
			"weights_are_separate": true,
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("built_with:    PLaMo 3 Translate\n")
	fmt.Printf("repo:          %s\n", modelstore.Repository)
	fmt.Printf("revision:      %s\n", modelstore.Revision)
	fmt.Printf("file:          %s\n", modelstore.Filename)
	fmt.Printf("size_bytes:    %d\n", modelstore.Size)
	fmt.Printf("sha256:        %s\n", modelstore.SHA256)
	fmt.Printf("download_url:  %s\n", modelstore.URL)
	fmt.Printf("license:       PLaMo Community License (weights; source is MIT)\n")
	fmt.Printf("license_ja:    %s\n", modelLicenseJA)
	fmt.Printf("license_en:    %s\n", modelLicenseEN)
	fmt.Printf("license_pinned: %s\n", modelstore.LicenseURL)
	return nil
}

func runModelVerify(args []string) error {
	fs := flag.NewFlagSet("model verify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", "models", "directory containing the GGUF file")
	fileName := fs.String("file", modelstore.Filename, "model file name")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	path := filepath.Join(*dir, *fileName)
	if err := verifyModelFile(path, modelstore.Size, modelstore.SHA256); err != nil {
		return fmt.Errorf("verify %s: %w", path, err)
	}
	fmt.Printf("%s: OK (%d bytes, sha256 %s)\n", path, modelstore.Size, modelstore.SHA256)
	return nil
}

func runModelDownload(args []string) error {
	fs := flag.NewFlagSet("model download", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dir := fs.String("dir", "models", "destination directory")
	acknowledge := fs.Bool("acknowledge-license", false, "explicitly acknowledge the PLaMo Community License")
	accept := fs.Bool("accept-license", false, "alias of --acknowledge-license")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if !*acknowledge && !*accept {
		fmt.Fprintf(os.Stderr, `Built with PLaMo 3 Translate.
The model weights are licensed under the PLaMo Community License, separately
from this program's MIT source license:
  %s
  %s
  %s

Private, hobby, academic, and other non-commercial use is permitted without
registration. Commercial use has separate conditions and may require
registration with Preferred Networks. Passing an acknowledgement flag here
does not register you and does not grant commercial rights.
`, modelstore.LicenseURL, modelLicenseJA, modelLicenseEN)
		return errors.New("model license was not acknowledged; review the license and rerun with --acknowledge-license")
	}

	fmt.Fprintf(os.Stderr, "Built with PLaMo 3 Translate. Model license: %s\n", modelstore.LicenseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	path, err := modelstore.Download(ctx, *dir)
	if err != nil {
		return fmt.Errorf("download model: %w", err)
	}
	fmt.Println(path)
	return nil
}

func verifyModelFile(path string, size int64, digest string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("model path is not a regular file")
	}
	if info.Size() != size {
		return fmt.Errorf("size mismatch: got %d, want %d", info.Size(), size)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, digest) {
		return fmt.Errorf("SHA256 mismatch: got %s, want %s", actual, digest)
	}
	return nil
}
