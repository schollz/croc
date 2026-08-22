package doctor

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/schollz/croc/v11/src/models"
	buildversion "github.com/schollz/croc/v11/src/version"
)

func assertStatus(t *testing.T, c Check, want Status) {
	t.Helper()
	if c.Status != want {
		t.Errorf("check %q: got status %v, want %v (detail: %q)", c.Name, c.Status, want, c.Detail)
	}
}

func TestGlyph(t *testing.T) {
	cases := map[Status]string{
		OK:   "✓",
		Warn: "!",
		Fail: "✗",
		Skip: "-",
	}
	for status, want := range cases {
		if got := glyph(status); got != want {
			t.Errorf("glyph(%v) = %q, want %q", status, got, want)
		}
	}
	if got := glyph(Status(99)); got != "?" {
		t.Errorf("glyph(unknown) = %q, want %q", got, "?")
	}
}

func TestStatusString(t *testing.T) {
	cases := map[Status]string{OK: "ok", Warn: "warn", Fail: "fail", Skip: "skip"}
	for status, want := range cases {
		if got := status.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", status, got, want)
		}
	}
}

func TestStatusMarshalJSON(t *testing.T) {
	b, err := json.Marshal(Warn)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `"warn"` {
		t.Errorf("json.Marshal(Warn) = %s, want %q", b, `"warn"`)
	}
}

func TestSplitRelayHostPort(t *testing.T) {
	cases := []struct {
		in       string
		wantHost string
		wantPort string
	}{
		{"croc.schollz.com:9009", "croc.schollz.com", "9009"},
		{"1.2.3.4:9009", "1.2.3.4", "9009"},
		{"myrelay.example.com", "myrelay.example.com", "9009"}, // no port -> default
		{"  spaced.example.com:1234  ", "spaced.example.com", "1234"},
		{"[::1]:9009", "::1", "9009"}, // IPv6 with port
		{"", "", ""},
	}
	for _, c := range cases {
		host, port := splitRelayHostPort(c.in)
		if host != c.wantHost || port != c.wantPort {
			t.Errorf("splitRelayHostPort(%q) = (%q, %q), want (%q, %q)",
				c.in, host, port, c.wantHost, c.wantPort)
		}
	}
}

func TestCheckProxyConfig(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want Status
	}{
		{"no proxy set", Options{}, Skip},
		{"bare socks5 host gets scheme prepended", Options{Socks5: "127.0.0.1:9050"}, OK},
		{"valid socks5 url", Options{Socks5: "socks5://127.0.0.1:9050"}, OK},
		{"wrong socks scheme (issue example)", Options{Socks5: "socks://127.0.0.1:9050"}, Fail},
		{"bare http host gets scheme prepended", Options{HTTPProxy: "proxy.corp:8080"}, OK},
		{"valid http url", Options{HTTPProxy: "http://proxy.corp:8080"}, OK},
		{"malformed http url", Options{HTTPProxy: "http://%zz"}, Fail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertStatus(t, checkProxyConfig(c.opts), c.want)
		})
	}
}

func TestCheckLocalOnly(t *testing.T) {
	assertStatus(t, checkLocalOnly(Options{OnlyLocal: false}), Skip)

	c := checkLocalOnly(Options{OnlyLocal: true})
	assertStatus(t, c, Warn)
	if !strings.Contains(c.Detail, models.DEFAULT_MULTICAST) {
		t.Errorf("expected default multicast address in detail, got %q", c.Detail)
	}

	c = checkLocalOnly(Options{OnlyLocal: true, MulticastAddress: "239.1.2.3"})
	if !strings.Contains(c.Detail, "239.1.2.3") {
		t.Errorf("expected custom multicast address in detail, got %q", c.Detail)
	}
}

func TestCheckVersion(t *testing.T) {
	c := checkVersion(Options{})
	assertStatus(t, c, OK)
	correctVersion := buildversion.Value
	if !strings.Contains(c.Name, correctVersion) {
		t.Errorf("expected version %q in name, got %q", correctVersion, c.Name)
	}
	if !strings.Contains(c.Detail, runtime.GOOS) {
		t.Errorf("expected platform %q in detail, got %q", runtime.GOOS, c.Detail)
	}
}

func TestCheckOutputDirWritable(t *testing.T) {
	dir := t.TempDir()

	t.Run("writable directory", func(t *testing.T) {
		assertStatus(t, checkOutDirWritable(Options{OutDir: dir}), OK)
	})

	t.Run("nonexistent path", func(t *testing.T) {
		missing := filepath.Join(dir, "does-not-exist-43u8cndlks89374")
		assertStatus(t, checkOutDirWritable(Options{OutDir: missing}), Fail)
	})

	t.Run("path is a file, not a dir", func(t *testing.T) {
		f := filepath.Join(dir, "a-file")
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertStatus(t, checkOutDirWritable(Options{OutDir: f}), Fail)
	})
}

func TestSkipBranches(t *testing.T) {
	assertStatus(t, checkRelay(Options{Relay: ""}), Skip)
	assertStatus(t, checkStoreReachable(Options{StoreURL: ""}), Skip)
}

func TestPrintHuman(t *testing.T) {
	r := Report{Checks: []Check{
		{Name: "croc version 11.1.1", Status: OK, Detail: "linux/amd64"},
		{Name: "relay TCP reachable", Status: OK},
		{Name: "output folder", Status: Fail, Detail: "/nope does not exist"},
	}}

	var buf bytes.Buffer
	r.PrintHuman(&buf)
	out := buf.String()

	// A check with a detail renders "glyph name: detail"
	if !strings.Contains(out, "✓ croc version 11.1.1: linux/amd64") {
		t.Errorf("missing formatted OK line:\n%s", out)
	}
	// A check with no detail renders "glyph name" with no trailing colon
	if !strings.Contains(out, "✓ relay TCP reachable\n") {
		t.Errorf("empty-detail line should have no colon:\n%s", out)
	}
	if !strings.Contains(out, "✗ output folder: /nope does not exist") {
		t.Errorf("missing formatted Fail line:\n%s", out)
	}
	// Summary should count the single failure
	if !strings.Contains(out, "1 failed") {
		t.Errorf("summary should report the failure:\n%s", out)
	}
}

func TestHasFailures(t *testing.T) {
	clean := Report{Checks: []Check{
		{Name: "a", Status: OK},
		{Name: "b", Status: Warn},
		{Name: "c", Status: Skip},
	}}
	if clean.HasFailures() {
		t.Error("report with no Fail checks should not report failures")
	}

	broken := Report{Checks: []Check{
		{Name: "a", Status: OK},
		{Name: "b", Status: Fail},
	}}
	if !broken.HasFailures() {
		t.Error("report containing a Fail check should report failures")
	}
}
