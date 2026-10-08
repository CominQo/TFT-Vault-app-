package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmpVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.2", "v0.1.3", -1}, {"0.1.3", "0.1.3", 0}, {"0.2.0", "0.1.9", 1}, {"1.0.0", "0.9.9", 1},
		{"0.1.10", "0.1.9", 1}, {"0.1", "0.1.0", 0}, {"1.0.0-beta", "1.0.0", -1}, {"1.0.0", "1.0.0-rc.1", 1},
		{"dev", "9.9.9", 0}, {"0.1.2", "garbage", 0}, {"v2", "v1.9.9", 1},
	}
	for _, c := range cases {
		if got := cmpVersion(c.a, c.b); got != c.want {
			t.Errorf("cmpVersion(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseSHA256(t *testing.T) {
	h := strings.Repeat("ab", 32)
	for _, in := range []string{h + "  TFT-Vault.exe\n", h + " *TFT-Vault.exe", strings.ToUpper(h), "\\" + h + " x"} {
		if got, err := parseSHA256(in); err != nil || got != h {
			t.Errorf("parseSHA256(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "abc", strings.Repeat("zz", 32)} {
		if _, err := parseSHA256(bad); err == nil {
			t.Errorf("parseSHA256(%q) should fail", bad)
		}
	}
}

// fakeGitHub serves one release with an exe + checksum, optionally corrupting the exe.
func fakeGitHub(t *testing.T, tag string, payload []byte, corrupt bool, opts ...string) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(payload)
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+updateRepo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "UA required", 403)
			return
		}
		assets := []ghAsset{{Name: updateAsset, Size: int64(len(payload)), URL: srv.URL + "/dl/exe"}, {Name: updateAsset + ".sha256", Size: 90, URL: srv.URL + "/dl/sum"}}
		for _, o := range opts {
			if o == "nosum" {
				assets = assets[:1]
			}
		}
		rel := ghRelease{Tag: tag, Body: "## New\n- thing", HTMLURL: "https://github.com/" + updateRepo + "/releases/tag/" + tag, Assets: assets}
		for _, o := range opts {
			if o == "prerelease" {
				rel.Prerelease = true
			}
		}
		json.NewEncoder(w).Encode(rel)
	})
	mux.HandleFunc("/dl/exe", func(w http.ResponseWriter, r *http.Request) {
		b := append([]byte(nil), payload...)
		if corrupt {
			b[len(b)/2] ^= 0xFF
		}
		w.Write(b)
	})
	mux.HandleFunc("/dl/sum", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), updateAsset)
	})
	mux.HandleFunc("/repos/"+updateRepo+"/releases/none", http.NotFound)
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func withAPI(t *testing.T, base string, install bool) {
	oldBase, oldInst := apiBase, installSupported
	apiBase, installSupported = base, install
	t.Cleanup(func() { apiBase, installSupported = oldBase, oldInst })
}

func TestCheckUpdate(t *testing.T) {
	payload := []byte(strings.Repeat("MZ-new-exe-", 5000))
	ctx := context.Background()

	srv := fakeGitHub(t, "v0.1.3", payload, false)
	withAPI(t, srv.URL, true)

	info, rel, err := checkUpdate(ctx, "0.1.2")
	if err != nil || !info.Available || info.Latest != "0.1.3" || !info.CanInstall || rel == nil || info.Size != int64(len(payload)) {
		t.Fatalf("expected an installable update, got %+v err=%v", info, err)
	}
	if !strings.Contains(info.Notes, "thing") || !strings.HasPrefix(info.PageURL, "https://github.com/"+updateRepo) {
		t.Errorf("notes/page url wrong: %+v", info)
	}
	if info, _, _ := checkUpdate(ctx, "0.1.3"); info.Available {
		t.Error("same version must not offer an update")
	}
	if info, _, _ := checkUpdate(ctx, "0.2.0"); info.Available {
		t.Error("newer local version must not offer an update")
	}
	if info, rel, err := checkUpdate(ctx, "dev"); !info.Dev || info.Available || rel != nil || err != nil {
		t.Errorf("dev build must skip updates: %+v", info)
	}
	withAPI(t, srv.URL, false)
	if info, _, _ := checkUpdate(ctx, "0.1.2"); !info.Available || info.CanInstall {
		t.Errorf("non-windows: update visible but not installable: %+v", info)
	}
	// no checksum asset => never auto-install
	srv2 := fakeGitHub(t, "v0.1.3", payload, false, "nosum")
	withAPI(t, srv2.URL, true)
	if info, _, _ := checkUpdate(ctx, "0.1.2"); !info.Available || info.CanInstall {
		t.Errorf("release without checksum must not be installable: %+v", info)
	}
	// prereleases are ignored
	srv3 := fakeGitHub(t, "v0.9.0", payload, false, "prerelease")
	withAPI(t, srv3.URL, true)
	if info, _, _ := checkUpdate(ctx, "0.1.2"); info.Available {
		t.Error("prerelease must not be offered")
	}
	// HTTP 404 = no release published yet; unreachable server = error
	mux := http.NewServeMux()
	mux.HandleFunc("/", http.NotFound)
	s404 := httptest.NewServer(mux)
	defer s404.Close()
	withAPI(t, s404.URL, true)
	if info, _, err := checkUpdate(ctx, "0.1.2"); err != nil || info.Available {
		t.Errorf("404 should mean 'no update': %+v %v", info, err)
	}
	withAPI(t, "http://127.0.0.1:1", true)
	if _, _, err := checkUpdate(ctx, "0.1.2"); err == nil {
		t.Error("unreachable server must return an error")
	}
	srvRL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer srvRL.Close()
	withAPI(t, srvRL.URL, true)
	if _, _, err := checkUpdate(ctx, "0.1.2"); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("403 should read as a rate limit, got %v", err)
	}
}

func TestValidAssetURL(t *testing.T) {
	ok := "https://github.com/" + updateRepo + "/releases/download/v1/TFT-Vault.exe"
	if !validAssetURL(ok) {
		t.Error("official release asset must be accepted")
	}
	for _, bad := range []string{
		"http://github.com/" + updateRepo + "/releases/download/v1/x.exe",
		"https://evil.example/" + updateRepo + "/releases/download/v1/x.exe",
		"https://github.com/someone-else/repo/releases/download/v1/x.exe",
		"https://github.com.evil.example/" + updateRepo + "/releases/download/v1/x.exe",
		"file:///c:/x.exe", "",
	} {
		if validAssetURL(bad) {
			t.Errorf("must reject %q", bad)
		}
	}
}

func TestDownloadVerified(t *testing.T) {
	payload := []byte(strings.Repeat("MZ-new-exe-", 20000))
	ctx := context.Background()
	dir := t.TempDir()

	srv := fakeGitHub(t, "v0.1.3", payload, false)
	withAPI(t, srv.URL, true)
	_, rel, _ := checkUpdate(ctx, "0.1.2")
	dest := filepath.Join(dir, "new.exe")
	var last, calls int64
	if err := downloadVerified(ctx, rel, dest, func(d, total int64) { last, calls = d, calls+1; _ = total }); err != nil {
		t.Fatalf("good download failed: %v", err)
	}
	if b, _ := os.ReadFile(dest); string(b) != string(payload) || last != int64(len(payload)) || calls == 0 {
		t.Errorf("file/progress wrong (last=%d calls=%d)", last, calls)
	}

	bad := fakeGitHub(t, "v0.1.3", payload, true) // body differs from the published checksum
	withAPI(t, bad.URL, true)
	_, rel2, _ := checkUpdate(ctx, "0.1.2")
	dest2 := filepath.Join(dir, "tampered.exe")
	err := downloadVerified(ctx, rel2, dest2, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered download must be rejected, got %v", err)
	}
	if _, statErr := os.Stat(dest2); statErr == nil {
		t.Error("a rejected download must not be left on disk")
	}
	if err := downloadVerified(ctx, &ghRelease{}, filepath.Join(dir, "x"), nil); err == nil {
		t.Error("release without assets must fail")
	}
}

func TestApplyUpdate(t *testing.T) {
	dir := t.TempDir()
	exe, nw := filepath.Join(dir, "app.exe"), filepath.Join(dir, "app.exe.new")
	os.WriteFile(exe, []byte("OLD"), 0o755)
	os.WriteFile(nw, []byte("NEW"), 0o755)
	os.WriteFile(exe+".old", []byte("ANCIENT"), 0o755) // stale leftover must not block the update
	if err := applyUpdate(exe, nw); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW" {
		t.Errorf("exe should now be NEW, is %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "OLD" {
		t.Errorf(".old should hold the previous version, has %q", b)
	}
	// rollback: new file missing => original restored
	dir2 := t.TempDir()
	exe2 := filepath.Join(dir2, "app.exe")
	os.WriteFile(exe2, []byte("KEEP"), 0o755)
	if err := applyUpdate(exe2, filepath.Join(dir2, "missing.new")); err == nil {
		t.Fatal("expected an error")
	}
	if b, _ := os.ReadFile(exe2); string(b) != "KEEP" {
		t.Errorf("a failed update must restore the original, got %q", b)
	}
}
