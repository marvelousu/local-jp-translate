package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelDownloadRequiresExplicitAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	err := runModelDownload([]string{"--dir", dir})
	if err == nil {
		t.Fatal("expected an acknowledgement error")
	}
	if !strings.Contains(err.Error(), "acknowledge-license") {
		t.Fatalf("err = %v", err)
	}
	if entries, readErr := os.ReadDir(dir); readErr != nil {
		t.Fatal(readErr)
	} else if len(entries) != 0 {
		t.Fatalf("download started without acknowledgement: %v", entries)
	}
}

func TestVerifyModelFile(t *testing.T) {
	content := []byte("model bytes")
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	digest := fmt.Sprintf("%x", sum)

	if err := verifyModelFile(path, int64(len(content)), digest); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := verifyModelFile(path, int64(len(content))+1, digest); err == nil {
		t.Fatal("expected size mismatch")
	}
	if err := verifyModelFile(path, int64(len(content)), strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected SHA256 mismatch")
	}
	if err := verifyModelFile(filepath.Join(t.TempDir(), "missing.gguf"), 1, digest); err == nil {
		t.Fatal("expected missing file error")
	}
}
