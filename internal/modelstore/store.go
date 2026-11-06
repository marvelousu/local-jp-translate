// Package modelstore installs the pinned official PLaMo GGUF without replacing
// an existing model with unverified bytes.
package modelstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const Filename = "plamo-3-translate-0.8b-Q4_K_M.gguf"
const Revision = "dbeb0856cb0b004b484f190bb71dd65876297013"
const SHA256 = "923b2f28a9856de1b8b3b610a7e59ded0b14d007466fd5dd526e6a9ecaadafdd"
const Size int64 = 573123200
const Repository = "pfnet/plamo-3-translate-0.8b-GGUF"
const URL = "https://huggingface.co/" + Repository + "/resolve/" + Revision + "/" + Filename
const LicenseURL = "https://huggingface.co/" + Repository + "/blob/" + Revision + "/LICENSE/ja"

// Download verifies an existing model or installs the immutable official model.
// The caller must present model license terms before starting a download.
func Download(ctx context.Context, directory string) (string, error) {
	client := &http.Client{Transport: downloadTransport(), CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 8 || req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("refusing unsafe model download redirect")
		}
		return nil
	}}
	return install(ctx, directory, URL, Filename, Size, SHA256, client)
}

func downloadTransport() *http.Transport {
	return &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second}
}

func verify(path string, size int64, digest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("model size/type mismatch")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("model SHA256 mismatch")
	}
	return nil
}

func install(ctx context.Context, directory, sourceURL, name string, size int64, digest string, client *http.Client) (string, error) {
	if filepath.Base(name) != name || size <= 0 {
		return "", errors.New("invalid model manifest")
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Host == "" {
		return "", errors.New("invalid model URL")
	}
	if err = os.MkdirAll(directory, 0755); err != nil {
		return "", err
	}
	destination := filepath.Join(directory, name)
	lock, err := os.OpenFile(destination+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("model installation lock %s: %w; remove this lock only after confirming no download is running", destination+".lock", err)
	}
	if _, err := fmt.Fprintf(lock, "pid=%d\nacquired=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339)); err != nil {
		lock.Close()
		os.Remove(destination + ".lock")
		return "", err
	}
	lock.Close()
	defer os.Remove(destination + ".lock")
	if _, err = os.Lstat(destination); err == nil {
		if err = verify(destination, size, digest); err != nil {
			return "", fmt.Errorf("existing model left unchanged: %w", err)
		}
		return destination, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "local-jp-translate/0.1")
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model download HTTP %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != size {
		return "", errors.New("model download length differs from pinned manifest")
	}
	temp, err := os.CreateTemp(directory, ".model-*.partial")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	h := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, h), io.LimitReader(response.Body, size+1))
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeErr := temp.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if written != size || hex.EncodeToString(h.Sum(nil)) != digest {
		return "", errors.New("download failed pinned model size/SHA256 verification")
	}
	if _, err = os.Lstat(destination); err == nil {
		return "", errors.New("model destination appeared during download; left unchanged")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err = os.Rename(temp.Name(), destination); err != nil {
		return "", err
	}
	return destination, nil
}
