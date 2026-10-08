package main

// Self-update from GitHub Releases. Standard library only.
//
// Release convention (the GitHub workflow in .github/workflows/build.yml does this for you):
//   - tag:     v0.1.3
//   - assets:  TFT-Vault.exe  and  TFT-Vault.exe.sha256
// The app asks GitHub for the latest release, compares it with the version baked in at build time
// (-ldflags "-X main.version=0.1.3"), downloads the exe, checks its SHA-256, swaps it in and restarts.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// version is injected at build time. "dev" (a local `wails dev` / untagged build) disables updates.
var version = "dev"

const (
	updateRepo     = "CominQo/TFT-Vault-app-"
	updateAsset    = "TFT-Vault.exe"
	maxDownload    = 300 << 20 // refuse absurdly large downloads
	releaseTimeout = 15 * time.Second
)

var (
	apiBase          = "https://api.github.com" // overridden in tests
	installSupported = runtime.GOOS == "windows"
	userAgent        = "TFT-Vault-Updater"
)

type ghAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}
type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Name       string    `json:"name"`
	Body       string    `json:"body"`
	HTMLURL    string    `json:"html_url"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

// UpdateInfo is what the UI sees. It deliberately contains no download URL:
// the frontend can only say "install", never "download this".
type UpdateInfo struct {
	Available  bool   `json:"available"`
	Dev        bool   `json:"dev"`
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Notes      string `json:"notes"`
	PageURL    string `json:"pageUrl"`
	Size       int64  `json:"size"`
	CanInstall bool   `json:"canInstall"`
}

func (r *ghRelease) asset(name string) *ghAsset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// ---------- version compare ----------

// parseVersion reads "v1.2.3" / "1.2.3-beta.1" into numbers plus a pre-release flag.
func parseVersion(s string) (nums [3]int, pre bool, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre, s = true, s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 || parts[0] == "" {
		return nums, pre, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nums, pre, false
		}
		nums[i] = n
	}
	return nums, pre, true
}

// cmpVersion returns -1, 0 or 1. Unparseable versions compare as equal (=> never offer an update).
func cmpVersion(a, b string) int {
	an, ap, aok := parseVersion(a)
	bn, bp, bok := parseVersion(b)
	if !aok || !bok {
		return 0
	}
	for i := 0; i < 3; i++ {
		if an[i] != bn[i] {
			if an[i] < bn[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case ap && !bp:
		return -1
	case !ap && bp:
		return 1
	}
	return 0
}

// ---------- GitHub ----------

func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 6 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && !strings.HasPrefix(apiBase, "http://") { // tests use plain http
				return errors.New("refusing a non-https redirect")
			}
			return nil
		},
	}
}

func getJSON(ctx context.Context, c *http.Client, u string, v any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent+"/"+version)
	res, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return res.StatusCode, nil
	}
	return res.StatusCode, json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(v)
}

// checkUpdate asks GitHub for the latest release and compares it with cur.
// The returned *ghRelease is kept by the caller so InstallUpdate can use it (never the UI).
func checkUpdate(ctx context.Context, cur string) (UpdateInfo, *ghRelease, error) {
	info := UpdateInfo{Current: cur}
	if _, _, ok := parseVersion(cur); !ok { // "dev" or anything unparseable
		info.Dev = true
		return info, nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()
	var rel ghRelease
	code, err := getJSON(ctx, newHTTPClient(releaseTimeout), apiBase+"/repos/"+updateRepo+"/releases/latest", &rel)
	if err != nil {
		return info, nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	switch {
	case code == http.StatusNotFound:
		return info, nil, nil // no release published yet
	case code == http.StatusForbidden || code == http.StatusTooManyRequests:
		return info, nil, errors.New("GitHub rate limit reached, try again later")
	case code != http.StatusOK:
		return info, nil, fmt.Errorf("GitHub answered HTTP %d", code)
	}
	if rel.Draft || rel.Prerelease {
		return info, nil, nil
	}
	info.Latest = strings.TrimPrefix(rel.Tag, "v")
	info.PageURL = rel.HTMLURL
	info.Notes = rel.Body
	if !strings.HasPrefix(info.PageURL, "https://github.com/"+updateRepo+"/") {
		info.PageURL = "https://github.com/" + updateRepo + "/releases/latest"
	}
	if cmpVersion(cur, rel.Tag) >= 0 {
		return info, nil, nil
	}
	info.Available = true
	exe, sum := rel.asset(updateAsset), rel.asset(updateAsset+".sha256")
	if exe != nil {
		info.Size = exe.Size
	}
	info.CanInstall = installSupported && exe != nil && sum != nil && validAssetURL(exe.URL) && validAssetURL(sum.URL)
	return info, &rel, nil
}

// Only ever download from this repository's release assets over https.
func validAssetURL(raw string) bool {
	if strings.HasPrefix(apiBase, "http://") && strings.HasPrefix(raw, apiBase) { // tests
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" &&
		strings.HasPrefix(u.Path, "/"+updateRepo+"/releases/download/")
}

// ---------- download + verify ----------

func parseSHA256(text string) (string, error) {
	f := strings.Fields(text)
	if len(f) == 0 {
		return "", errors.New("empty checksum file")
	}
	h := strings.ToLower(strings.TrimPrefix(f[0], "\\")) // sha256sum prefixes a backslash for escaped names
	if len(h) != 64 {
		return "", errors.New("malformed checksum file")
	}
	if _, err := hex.DecodeString(h); err != nil {
		return "", errors.New("malformed checksum file")
	}
	return h, nil
}

func fetch(ctx context.Context, c *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent+"/"+version)
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("download failed: HTTP %d", res.StatusCode)
	}
	return res, nil
}

// downloadVerified downloads the exe to dest and only keeps it if its SHA-256 matches the published checksum.
func downloadVerified(ctx context.Context, rel *ghRelease, dest string, progress func(done, total int64)) error {
	exe, sum := rel.asset(updateAsset), rel.asset(updateAsset+".sha256")
	if exe == nil || sum == nil || !validAssetURL(exe.URL) || !validAssetURL(sum.URL) {
		return errors.New("this release has no verifiable installer")
	}
	c := newHTTPClient(0) // no overall timeout: the context bounds it
	r, err := fetch(ctx, c, sum.URL)
	if err != nil {
		return err
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	r.Body.Close()
	if err != nil {
		return err
	}
	want, err := parseSHA256(string(b))
	if err != nil {
		return err
	}

	res, err := fetch(ctx, c, exe.URL)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	total := res.ContentLength
	if total <= 0 {
		total = exe.Size
	}
	if total > maxDownload {
		return errors.New("download is unexpectedly large, refusing")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("cannot write next to the app (try a folder you own): %w", err)
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if done += int64(n); done > maxDownload {
				f.Close()
				os.Remove(dest)
				return errors.New("download is unexpectedly large, refusing")
			}
			h.Write(buf[:n])
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(dest)
				return werr
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(dest)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(dest)
		return err
	}
	if exe.Size > 0 && done != exe.Size {
		os.Remove(dest)
		return errors.New("download was incomplete")
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		os.Remove(dest)
		return errors.New("checksum mismatch: the download is corrupted or has been tampered with")
	}
	return nil
}

// ---------- swap the running exe ----------

// applyUpdate moves the running exe aside (Windows allows renaming a running exe, not overwriting it),
// moves the new one into its place and rolls back if that fails.
func applyUpdate(exe, newPath string) error {
	old := exe + ".old"
	_ = os.Remove(old) // leftover from the previous update
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("cannot replace the running app (is it in a protected folder?): %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(old, exe)
		return fmt.Errorf("cannot put the new version in place: %w", err)
	}
	return nil
}

// cleanupOldBinary removes the .old file left by the last update (call on startup).
func cleanupOldBinary() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
		_ = os.Remove(exe + ".new")
	}
}
