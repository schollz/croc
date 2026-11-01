//go:build !windows

package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestElevationSelectionAndFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("selection is tested as an ordinary user")
	}
	for _, available := range []string{"sudo", "doas", "none"} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fail=%v", available, fail), func(t *testing.T) {
				calls := 0
				look := func(name string) (string, error) {
					if name == available {
						return "/tools/" + name, nil
					}
					return "", exec.ErrNotFound
				}
				runner := func(_ context.Context, tool string, args []string) error {
					calls++
					if tool != "/tools/"+available || strings.Join(args, "|") != "-n|--|/prefix with spaces/croc|update|--internal-install-version|11.6.0" {
						t.Fatalf("%s %q", tool, args)
					}
					if fail {
						return errors.New("authentication denied")
					}
					return nil
				}
				err := elevateUnix(t.Context(), "/prefix with spaces/croc", []string{"update", "--internal-install-version", "11.6.0"}, look, runner, false)
				if available == "none" {
					if err == nil || calls != 0 {
						t.Fatal("missing elevation tools were ignored")
					}
					return
				}
				if calls != 1 || (err != nil) != fail {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
				if err != nil && !strings.Contains(err.Error(), "run manually in a terminal") {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestPermissionFailureEscalatesOnlyOnce(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires a permission boundary")
	}
	dir := t.TempDir()
	executable := writeFixture(t, dir, Croc, []byte("not executed: the helper is missing"))
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			t.Error("download attempted before acquiring write permission")
		}
		fmt.Fprint(w, `{"tag_name":"v11.6.0"}`)
	}))
	defer server.Close()
	for _, internal := range []string{"", "11.6.0"} {
		calls := 0
		var output bytes.Buffer
		err := (Options{Tool: Croc, Version: "11.6.0", Executable: executable, Invoked: executable, Yes: true, Writer: &output, LatestEndpoint: server.URL + "/latest", InstallVersion: internal, Elevate: func(_ context.Context, exe string, args []string) error {
			calls++
			if exe != executable || strings.Join(args, "|") != "update|--internal-install-version|11.6.0" {
				t.Fatalf("wrong elevated operation: %s %q", exe, args)
			}
			return errors.New("authentication denied")
		}}).Run(t.Context())
		if err == nil {
			t.Fatal("permission failure ignored")
		}
		want := 1
		if internal != "" {
			want = 0
		}
		if calls != want {
			t.Fatalf("elevation called %d times, wanted %d", calls, want)
		}
	}
}

// Enabled on the disposable Linux CI runner, where passwordless sudo is
// available. A real root-owned prefix tests both commands across the boundary.
func TestProtectedInstallation(t *testing.T) {
	if os.Getenv("CROC_TEST_PROTECTED_INSTALL") != "1" || os.Geteuid() == 0 {
		t.Skip("requires disposable Linux CI with passwordless sudo")
	}
	if output, err := exec.Command("sudo", "-n", "true").CombinedOutput(); err != nil {
		t.Fatalf("CI sudo unavailable: %v %s", err, output)
	}
	oldCroc := buildFixture(t, Croc, "11.5.0")
	newCroc := buildFixture(t, Croc, "11.6.0")
	newHelper := buildFixture(t, Helper, "11.6.0")
	archive := archiveFixture(t, map[string][]byte{BinaryName(Croc): newCroc, BinaryName(Helper): newHelper})
	asset, _ := AssetName("11.6.0")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/latest":
			fmt.Fprint(w, `{"tag_name":"v11.6.0"}`)
		case strings.HasSuffix(r.URL.Path, "_checksums.txt"):
			fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive), asset)
		default:
			w.Write(archive)
		}
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "protected prefix")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	croc := writeFixture(t, dir, Croc, oldCroc)
	if err := os.WriteFile(filepath.Join(dir, ".test-release-url"), []byte(server.URL), 0o644); err != nil {
		t.Fatal(err)
	}
	root := func(args ...string) {
		t.Helper()
		output, err := exec.Command("sudo", append([]string{"-n", "--"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("sudo %v: %v %s", args, err, output)
		}
	}
	root("chown", "-R", "0:0", dir)
	defer root("chown", "-R", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), dir)
	for _, command := range [][]string{{croc, "update", "--yes"}, {filepath.Join(dir, BinaryName(Helper)), "--path", croc, "--yes"}} {
		output, err := exec.Command(command[0], command[1:]...).CombinedOutput()
		if err != nil {
			t.Fatalf("protected upgrade: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "Administrator permission is required") {
			t.Fatalf("did not elevate: %s", output)
		}
	}
	for _, tool := range []string{Croc, Helper} {
		if got, err := installedVersion(t.Context(), filepath.Join(dir, BinaryName(tool)), tool); err != nil || got != "11.6.0" {
			t.Fatalf("%s: %s %v", tool, got, err)
		}
	}
}
