package doctor

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/schollz/croc/v11/src/diskusage"
	"github.com/schollz/croc/v11/src/models"
	"github.com/schollz/croc/v11/src/utils"
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
	panic("unimplemented")
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
	Relay     string `json:"relay"`
	Pass      string `json:"pass"`
	OutDir    string `json:"outDir"`
	StoreURL  string `json:"storeUrl"`
	Socks5    string `json:"socks5"`
	HTTPProxy string `json:"httpProxy"`
	Version   string `json:"version"`
}

func checkVersion(opts Options) Check {
	return Check{
		Name:   "croc version " + opts.Version,
		Status: OK,
		Detail: runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func checkConfigDirExistenceAndPermissions(_ Options) Check {
	configDir, _ := utils.GetConfigDir(false)
	info, err := os.Stat(configDir)
	if err != nil {
		return Check{
			Name:   "Config Dir",
			Status: Warn,
			Detail: err.Error(),
		}
	}

	return checkPermission("Config dir", configDir, info.Mode().Perm(), 0o600)
}

func checkRememberedConfigFilesReadableAndNotOverPermissive(_ Options) (checks []Check) {
	checks = append(checks, checkConfigFile("Send Config File", utils.GetSendConfigFile(false)))
	receiveConfigFilePath, _ := utils.GetReceiveConfigFile(false)
	checks = append(checks, checkConfigFile("Receive Config File", receiveConfigFilePath))
	checks = append(checks, checkConfigFile("Send Config File", utils.GetClassicConfigFile(false)))
	return
}

func checkConfigFile(fileDescription string, filePath string) Check {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return Check{
			Name:   fileDescription + " does not exist yet",
			Status: OK,
			Detail: "",
		}
	}

	permissionCheck := checkPermission(fileDescription, filePath, info.Mode().Perm(), 0o600)
	if permissionCheck.Status == OK {
		_, fileReadErr := os.ReadFile(filePath)
		if fileReadErr != nil {
			return Check{
				Name:   "Error when reading " + fileDescription,
				Status: Warn,
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
			Detail: filePath + " has permission code " + permGotten.String() + " but should have " + permWanted.String(),
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
			return Check{Name: name, Status: Warn, Detail: outDir + " does not exist"}
		}
		return Check{Name: name, Status: Warn, Detail: fmt.Sprintf("%s: %v", outDir, err)}
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

func checkRelayResolves(opts Options) Check {
	name := "relay resolves"
	host, port := splitRelayHostPort(opts.Relay)
	if host == "" {
		return Check{Name: name, Status: Skip, Detail: "no relay configured"}
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("%s: %v", host, err)}
	}
	return Check{
		Name:   name,
		Status: OK,
		Detail: fmt.Sprintf("%s:%s -> %s", host, port, strings.Join(addrs, ", ")),
	}
}

func Run(opts Options) (report Report) {
	report.Checks = []Check{checkVersion(opts), checkConfigDirExistenceAndPermissions(opts)}
	report.Checks = append(report.Checks, checkRememberedConfigFilesReadableAndNotOverPermissive(opts)...)
	report.Checks = append(report.Checks, checkOutDirWritable(opts), checkRelayResolves(opts))
	return
}
