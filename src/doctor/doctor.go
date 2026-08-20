package doctor

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/magisterquis/connectproxy"
	"github.com/schollz/croc/v11/src/diskusage"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/tcp"
	"github.com/schollz/croc/v11/src/utils"
	buildversion "github.com/schollz/croc/v11/src/version"
	"golang.org/x/net/proxy"
)

type Status int

const (
	OK Status = iota
	Warn
	Fail
	Skip
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	case Skip:
		return "skip"
	default:
		return "unknown"
	}
}

func (s Status) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(s.String())), nil
}

func glyph(s Status) string {
	switch s {
	case OK:
		return "✓"
	case Warn:
		return "!"
	case Fail:
		return "✗"
	case Skip:
		return "-"
	default:
		return "?"
	}
}

type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"` // "ok" | "warn" | "fail" | "skip"
	Detail string `json:"detail"`
}

type Report struct {
	Checks []Check `json:"checks"`
}

func (report Report) HasFailures() bool {
	for _, check := range report.Checks {
		if check.Status == Fail {
			return true
		}
	}
	return false
}

// "<glyph> <name>[: <detail>]"
func (r *Report) PrintHuman(w io.Writer) {
	var ok, warn, fail, skip int
	for _, check := range r.Checks {
		line := check.Name
		if check.Detail != "" {
			line += ": " + check.Detail
		}
		fmt.Fprintf(w, "%s %s\n", glyph(check.Status), line)

		switch check.Status {
		case OK:
			ok++
		case Warn:
			warn++
		case Fail:
			fail++
		case Skip:
			skip++
		}
	}

	fmt.Fprintf(w, "\n%d ok", ok)
	if warn > 0 {
		fmt.Fprintf(w, ", %d warning(s)", warn)
	}
	if fail > 0 {
		fmt.Fprintf(w, ", %d failed", fail)
	}
	if skip > 0 {
		fmt.Fprintf(w, ", %d skipped", skip)
	}
	fmt.Fprintln(w)
}

type Options struct {
	Relay            string `json:"relay"`
	Pass             string `json:"pass"`
	OutDir           string `json:"outDir"`
	StoreURL         string `json:"storeUrl"`
	Socks5           string `json:"socks5"`
	HTTPProxy        string `json:"httpProxy"`
	OnlyLocal        bool   `json:"onlyLocal"`
	MulticastAddress string `json:"multiCastAddress"`
}

func checkVersion(opts Options) Check {
	return Check{
		Name:   "croc version " + buildversion.Value,
		Status: OK,
		Detail: runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func checkConfigDirExistenceAndPermissions(_ Options) Check {
	name := "config dir"
	configDir, _ := utils.GetConfigDir(false)
	info, err := os.Stat(configDir)
	if err != nil {
		return Check{
			Name:   name,
			Status: Fail,
			Detail: fmt.Sprintf("os.Stat threw error for config dir %s: '%v'", configDir, err.Error()),
		}
	}

	return checkPermission(name, configDir, info.Mode().Perm(), 0o700)
}

func checkRememberedConfigFilesReadableAndNotOverPermissive(_ Options) (checks []Check) {
	checks = append(checks, checkConfigFile("send config file", utils.GetSendConfigFile(false)))
	receiveConfigFilePath, _ := utils.GetReceiveConfigFile(false)
	checks = append(checks, checkConfigFile("receive config file", receiveConfigFilePath))
	checks = append(checks, checkConfigFile("classic config file", utils.GetClassicConfigFile(false)))
	return
}

func checkConfigFile(fileDescription string, filePath string) Check {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return Check{
			Name:   fileDescription + " does not exist",
			Status: Skip,
			Detail: "",
		}
	}

	permissionCheck := checkPermission(fileDescription, filePath, info.Mode().Perm(), 0o600)
	if permissionCheck.Status == OK {
		_, fileReadErr := os.ReadFile(filePath)
		if fileReadErr != nil {
			return Check{
				Name:   "Error when reading " + fileDescription,
				Status: Fail,
				Detail: fileReadErr.Error(),
			}
		}
	}
	return permissionCheck
}

func checkPermission(fileDescription string, filePath string, permGotten os.FileMode, permWanted os.FileMode) Check {
	if permGotten != permWanted {
		return Check{
			Name:   fileDescription + " has undesired permissions",
			Status: Warn,
			Detail: filePath + " has permissions " + permGotten.String() + " but should have " + permWanted.String(),
		}
	}
	return Check{
		Name:   fileDescription + " exists with permissions",
		Status: OK,
		Detail: permWanted.String(),
	}
}

func checkOutDirWritable(opts Options) Check {
	outDir := opts.OutDir
	if outDir == "" {
		outDir = "."
	}

	name := "output folder"

	info, err := os.Stat(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Check{Name: name, Status: Fail, Detail: outDir + " does not exist"}
		}
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("%s: '%v'", outDir, err)}
	}
	if !info.IsDir() {
		return Check{Name: name, Status: Fail, Detail: outDir + " is not a directory"}
	}

	f, err := os.CreateTemp(outDir, ".croc-doctor-*")
	if err != nil {
		return Check{Name: name, Status: Fail, Detail: "cannot write to " + outDir + ": " + err.Error()}
	}
	f.Close()
	os.Remove(f.Name())

	avail := diskusage.NewDiskUsage(outDir).Available()
	absPath, err := filepath.Abs(outDir)
	if err != nil {
		absPath = outDir
	}
	return Check{
		Name:   name,
		Status: OK,
		Detail: fmt.Sprintf("%s (%s free)", absPath, utils.ByteCountDecimalUnsigned(avail)),
	}
}

func splitRelayHostPort(relay string) (host, port string) {
	relay = strings.TrimSpace(relay)
	if relay == "" {
		return "", ""
	}
	host, port, err := net.SplitHostPort(relay)
	if err != nil {
		return relay, models.DEFAULT_PORT
	}
	return host, port
}

func checkRelay(opts Options) Check {
	name := "relay"
	host, port := splitRelayHostPort(opts.Relay)
	if host == "" {
		return Check{Name: name, Status: Skip, Detail: "no relay configured"}
	}

	_, err := net.LookupHost(host)
	if err != nil {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("LookupHost failed for %s: '%v'", host, err)}
	}

	address := net.JoinHostPort(host, port)
	// Is the port open at all?
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("Dialing %s failed with error: '%v'", address, err)}
	}
	conn.Close()

	// Full Croc Handshake
	c, _, _, err := tcp.ConnectToTCPServer(address, opts.Pass, "croc-doctor-probe", 5*time.Second)
	if c != nil {
		c.Close()
	}
	if err != nil {
		return Check{
			Name:   name,
			Status: Fail,
			Detail: fmt.Sprintf("port open but handshake failed with error: '%v'", err),
		}
	}
	return Check{Name: name, Status: OK, Detail: fmt.Sprintf("TCP port at %s is open and handshake worked", address)}
}

func checkProxyConfig(opts Options) Check {
	name := "proxy configuration"

	if opts.Socks5 == "" && opts.HTTPProxy == "" {
		return Check{Name: name, Status: Skip, Detail: "no proxy set"}
	}

	if opts.Socks5 != "" {
		raw := opts.Socks5
		if !strings.Contains(raw, "://") {
			raw = "socks5://" + raw
		}
		url, err := url.Parse(raw)
		if err != nil {
			return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("SOCKS5_PROXY (%v) parsing threw error: '%v'", raw, err)}
		}
		if _, err := proxy.FromURL(url, proxy.Direct); err != nil {
			return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("SOCKS5_PROXY (%v) is set but malformed: '%v'", raw, err)}
		}
	}

	if opts.HTTPProxy != "" {
		raw := opts.HTTPProxy
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		url, err := url.Parse(raw)
		if err != nil {
			return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("HTTP_PROXY (%v) parsing threw error: '%v'", raw, err)}
		}
		if _, err := connectproxy.New(url, proxy.Direct); err != nil {
			return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("HTTP_PROXY (%v) is set but malformed: '%v'", raw, err)}
		}
	}

	return Check{Name: name, Status: OK, Detail: "proxy settings parsed"}
}

func checkLocalOnly(opts Options) Check {
	name := "local-only discovery"
	if !opts.OnlyLocal {
		return Check{Name: name, Status: Skip, Detail: "not in local-only mode"}
	}
	addr := opts.MulticastAddress
	if addr == "" {
		addr = "239.255.255.250"
	}
	return Check{
		Name:   name,
		Status: Warn,
		Detail: fmt.Sprintf("local-only mode relies on multicast %s, which many networks (guest/enterprise Wi-Fi, VPNs) block", addr),
	}
}

func checkStoreReachable(opts Options) Check {
	name := "stored-transfer service"
	storeUrl := strings.TrimSpace(opts.StoreURL)
	if storeUrl == "" {
		return Check{Name: name, Status: Skip, Detail: "no --store-url set"}
	}

	url, err := url.Parse(storeUrl)
	if err != nil || (url.Scheme != "http" && url.Scheme != "https") {
		return Check{Name: name, Status: Fail, Detail: "invalid store URL: " + storeUrl}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(storeUrl)
	if err != nil {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("Couldn't reach %s: '%v'", storeUrl, err)}
	}
	resp.Body.Close()

	return Check{Name: name, Status: OK, Detail: fmt.Sprintf("%s responded (HTTP %d)", storeUrl, resp.StatusCode)}
}

func Run(opts Options) (report Report) {
	report.Checks = []Check{checkVersion(opts), checkConfigDirExistenceAndPermissions(opts)}
	report.Checks = append(report.Checks, checkRememberedConfigFilesReadableAndNotOverPermissive(opts)...)
	report.Checks = append(report.Checks, checkOutDirWritable(opts), checkRelay(opts), checkProxyConfig(opts), checkLocalOnly(opts), checkStoreReachable(opts))
	return
}
