package utils

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cespare/xxhash/v2"
	"github.com/kalafut/imohash"
	"github.com/minio/highwayhash"
	"github.com/schollz/croc/v11/src/codephrase"
	log "github.com/schollz/croc/v11/src/logger"
	"github.com/schollz/croc/v11/src/receivefs"
	"github.com/schollz/croc/v11/src/termui"
	"github.com/schollz/progressbar/v3"
)

const NbPinNumbers = 4

const maxProgressFilenameRunes = 20
const minHashProgressSize int64 = 200 * 1024 * 1024

func shouldShowHashProgress(requested bool, size int64) bool {
	return requested && size >= minHashProgressSize
}

func shortenProgressFilename(fname string) string {
	fnameRunes := []rune(path.Base(fname))
	if len(fnameRunes) > maxProgressFilenameRunes {
		return string(fnameRunes[:maxProgressFilenameRunes]) + "..."
	}
	return string(fnameRunes)
}

// Get or create home directory
func GetConfigDir(requireValidPath bool) (homedir string, err error) {
	if envHomedir, isSet := os.LookupEnv("CROC_CONFIG_DIR"); isSet {
		homedir = envHomedir
	} else if xdgConfigHome, isSet := os.LookupEnv("XDG_CONFIG_HOME"); isSet {
		homedir = path.Join(xdgConfigHome, "croc")
	} else {
		homedir, err = os.UserHomeDir()
		if err != nil {
			if !requireValidPath {
				err = nil
				homedir = ""
			}
			return
		}
		homedir = path.Join(homedir, ".config", "croc")
	}

	if requireValidPath {
		if _, err = os.Stat(homedir); os.IsNotExist(err) {
			err = os.MkdirAll(homedir, 0o700)
		}
	}
	return
}

// Exists reports whether the named file or directory exists.
func Exists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// UnusedFilename returns a filename in folder that does not exist yet,
// appending " (1)", " (2)", etc. before the extension of name until an
// unused one is found.
func UnusedFilename(folder, name string) string {
	if !Exists(path.Join(folder, name)) {
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if !Exists(path.Join(folder, candidate)) {
			return candidate
		}
	}
}

// stdinReader is shared by all prompts so that piped or typed-ahead
// answers buffered past one line survive to the next prompt.
var stdinReader = bufio.NewReader(os.Stdin)

// GetInput returns one line of input from stdin with a given prompt,
// with surrounding whitespace trimmed. On read error (e.g. closed or
// exhausted stdin) the returned string is empty, so callers that treat
// an empty answer as consent must check the error.
func GetInput(prompt string) (string, error) {
	output, colorEnabled := termui.Output(os.Stderr)
	fmt.Fprint(output, termui.PromptChoices(prompt, colorEnabled))
	text, err := stdinReader.ReadString('\n')
	text = strings.TrimSpace(text)
	if errors.Is(err, io.EOF) && text != "" {
		// a final line without a trailing newline is still a valid answer
		err = nil
	}
	return text, err
}

// HashFile returns the hash of a file or, in case of a symlink, the
// SHA256 hash of its target. Takes an argument to specify the algorithm to use.
func HashFile(fname string, algorithm string, showProgress ...bool) (hash256 []byte, err error) {
	doShowProgress := false
	if len(showProgress) > 0 {
		doShowProgress = showProgress[0]
	}
	var fstats os.FileInfo
	fstats, err = os.Lstat(fname)
	if err != nil {
		return nil, err
	}
	if fstats.Mode()&os.ModeSymlink != 0 {
		var target string
		target, err = os.Readlink(fname)
		if err != nil {
			return nil, err
		}
		return []byte(SHA256(target)), nil
	}
	doShowProgress = shouldShowHashProgress(doShowProgress, fstats.Size())
	switch algorithm {
	case "imohash":
		return IMOHashFile(fname)
	case "md5":
		return MD5HashFile(fname, doShowProgress)
	case "xxhash":
		return XXHashFile(fname, doShowProgress)
	case "highway":
		return HighwayHashFile(fname, doShowProgress)
	}
	err = fmt.Errorf("unspecified algorithm")
	return
}

// HighwayHashFile returns highwayhash of a file
func HighwayHashFile(fname string, doShowProgress bool) (hashHighway []byte, err error) {
	f, err := os.Open(fname)
	if err != nil {
		return
	}
	defer f.Close()
	key, err := hex.DecodeString("1553c5383fb0b86578c3310da665b4f6e0521acf22eb58a99532ffed02a6b115")
	if err != nil {
		return
	}
	h, err := highwayhash.New(key)
	if err != nil {
		err = fmt.Errorf("could not create highwayhash: %s", err.Error())
		return
	}
	if doShowProgress {
		stat, _ := f.Stat()
		fnameShort := shortenProgressFilename(fname)
		bar := progressbar.NewOptions64(stat.Size(),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetDescription(fmt.Sprintf("Hashing %s", fnameShort)),
			progressbar.OptionClearOnFinish(),
			progressbar.OptionFullWidth(),
		)
		if _, err = io.Copy(io.MultiWriter(h, bar), f); err != nil {
			return
		}
	} else {
		if _, err = io.Copy(h, f); err != nil {
			return
		}
	}

	hashHighway = h.Sum(nil)
	return
}

// MD5HashFile returns MD5 hash
func MD5HashFile(fname string, doShowProgress bool) (hash256 []byte, err error) {
	f, err := os.Open(fname)
	if err != nil {
		return
	}
	defer f.Close()

	h := md5.New()
	if doShowProgress {
		stat, _ := f.Stat()
		fnameShort := shortenProgressFilename(fname)
		bar := progressbar.NewOptions64(stat.Size(),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetDescription(fmt.Sprintf("Hashing %s", fnameShort)),
			progressbar.OptionClearOnFinish(),
			progressbar.OptionFullWidth(),
		)
		if _, err = io.Copy(io.MultiWriter(h, bar), f); err != nil {
			return
		}
	} else {
		if _, err = io.Copy(h, f); err != nil {
			return
		}
	}

	hash256 = h.Sum(nil)
	return
}

var imofull = imohash.NewCustom(0, 0)
var imopartial = imohash.NewCustom(16*16*8*1024, 128*1024)

// IMOHashFile returns imohash
func IMOHashFile(fname string) (hash []byte, err error) {
	b, err := imopartial.SumFile(fname)
	hash = b[:]
	return
}

// IMOHashFileFull returns imohash of full file
func IMOHashFileFull(fname string) (hash []byte, err error) {
	b, err := imofull.SumFile(fname)
	hash = b[:]
	return
}

// XXHashFile returns the xxhash of a file
func XXHashFile(fname string, doShowProgress bool) (hash256 []byte, err error) {
	f, err := os.Open(fname)
	if err != nil {
		return
	}
	defer f.Close()

	h := xxhash.New()
	if doShowProgress {
		stat, _ := f.Stat()
		fnameShort := shortenProgressFilename(fname)
		bar := progressbar.NewOptions64(stat.Size(),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetDescription(fmt.Sprintf("Hashing %s", fnameShort)),
			progressbar.OptionClearOnFinish(),
			progressbar.OptionFullWidth(),
		)
		if _, err = io.Copy(io.MultiWriter(h, bar), f); err != nil {
			return
		}
	} else {
		if _, err = io.Copy(h, f); err != nil {
			return
		}
	}

	hash256 = h.Sum(nil)
	return
}

// SHA256 returns sha256 sum
func SHA256(s string) string {
	sha := sha256.New()
	sha.Write([]byte(s))
	return hex.EncodeToString(sha.Sum(nil))
}

// PublicIP returns public ip address
func PublicIP() (ip string, err error) {
	// ask ipv4.icanhazip.com for the public ip
	// by making http request
	// if the request fails, return nothing
	resp, err := http.Get("http://ipv4.icanhazip.com")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// read the body of the response
	// and return the ip address
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	ip = strings.TrimSpace(buf.String())

	return
}

// LocalIP returns local ip address
func LocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Error(err)
		return ""
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP.String()
}

// GenerateRandomPin returns a randomly generated pin with set length
func GenerateRandomPin() string {
	var s strings.Builder
	max := new(big.Int)
	max.SetInt64(9)
	for range NbPinNumbers {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		s.WriteString(fmt.Sprintf("%d", v))
	}
	return s.String()
}

// GetRandomName returns a random three-word croc code.
//
// It retains its historical signature for callers outside croc. New code that
// needs to handle a random-source failure should call codephrase.Generate.
func GetRandomName() string {
	name, err := codephrase.Generate()
	if err != nil {
		panic(err)
	}
	return name
}

// ByteCountDecimal converts bytes to human readable byte string
func ByteCountDecimal(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

// MissingChunks returns the positions of missing chunks.
// If file doesn't exist, it returns an empty chunk list (all chunks).
// If the file size is not the same as requested, it returns an empty chunk list (all chunks).
func MissingChunks(fname string, fsize int64, chunkSize int) (chunkRanges []int64) {
	f, err := os.Open(fname)
	if err != nil {
		return
	}
	defer f.Close()

	fstat, err := os.Stat(fname)
	if err != nil || fstat.Size() != fsize {
		return
	}

	// Show progress bar for large files (> 10MB)
	var bar *progressbar.ProgressBar
	showProgress := fsize > 10*1024*1024
	if showProgress {
		fnameShort := shortenProgressFilename(fname)
		bar = progressbar.NewOptions64(fsize,
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetDescription(fmt.Sprintf("Checking %s", fnameShort)),
			progressbar.OptionClearOnFinish(),
			progressbar.OptionFullWidth(),
			progressbar.OptionThrottle(100*time.Millisecond),
		)
	}

	buffer := make([]byte, chunkSize)
	var currentLocation int64
	var runStart, runCount int64
	flushRun := func() {
		if runCount == 0 {
			return
		}
		if len(chunkRanges) == 0 {
			chunkRanges = append(chunkRanges, int64(chunkSize))
		}
		chunkRanges = append(chunkRanges, runStart, runCount)
		runCount = 0
	}
	for currentLocation < fsize {
		bytesread, readErr := io.ReadFull(f, buffer)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return nil
		}
		if bytesread == 0 {
			break
		}
		if allZero(buffer[:bytesread]) {
			if runCount == 0 {
				runStart = currentLocation
			}
			runCount++
		} else {
			flushRun()
		}
		currentLocation += int64(bytesread)
		if showProgress && bar != nil {
			bar.Add(bytesread)
		}
		if readErr != nil {
			break
		}
	}
	flushRun()
	if showProgress && bar != nil {
		bar.Finish()
	}
	return
}

func allZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// ChunkRangesCount returns the number of chunks represented by ranges. An
// empty range list means the whole file, matching the transfer protocol.
func ChunkRangesCount(chunkRanges []int64, fileSize, defaultChunkSize int64) int {
	if fileSize <= 0 || defaultChunkSize <= 0 {
		return 0
	}
	if len(chunkRanges) == 0 {
		return int((fileSize + defaultChunkSize - 1) / defaultChunkSize)
	}
	var count int64
	for i := 2; i < len(chunkRanges); i += 2 {
		if chunkRanges[i] > 0 {
			count += chunkRanges[i]
		}
	}
	return int(count)
}

// ChunkRangesBytes returns the exact number of file bytes represented by
// ranges, including a possibly-short final chunk.
func ChunkRangesBytes(chunkRanges []int64, fileSize, defaultChunkSize int64) int64 {
	if fileSize <= 0 {
		return 0
	}
	if len(chunkRanges) == 0 {
		return fileSize
	}
	chunkSize := chunkRanges[0]
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	var total int64
	for i := 1; i+1 < len(chunkRanges); i += 2 {
		start, count := chunkRanges[i], chunkRanges[i+1]
		if start < 0 || start >= fileSize || count <= 0 {
			continue
		}
		end := min(start+count*chunkSize, fileSize)
		if end > start {
			total += end - start
		}
	}
	return total
}

// ChunkRangesContain reports whether the chunk at position is requested.
// An empty range list requests every chunk.
func ChunkRangesContain(chunkRanges []int64, position int64) bool {
	if len(chunkRanges) == 0 {
		return true
	}
	chunkSize := chunkRanges[0]
	if chunkSize <= 0 {
		return false
	}
	pairs := (len(chunkRanges) - 1) / 2
	low, high := 0, pairs
	for low < high {
		mid := low + (high-low)/2
		if chunkRanges[1+mid*2] <= position {
			low = mid + 1
		} else {
			high = mid
		}
	}
	if low == 0 {
		return false
	}
	index := 1 + (low-1)*2
	start, count := chunkRanges[index], chunkRanges[index+1]
	return count > 0 && position < start+count*chunkSize
}

// ChunkRangesToChunks converts chunk ranges to list
func ChunkRangesToChunks(chunkRanges []int64) (chunks []int64) {
	if len(chunkRanges) == 0 {
		return
	}
	chunkSize := chunkRanges[0]
	count := 0
	for i := 2; i < len(chunkRanges); i += 2 {
		if chunkRanges[i] > 0 {
			count += int(chunkRanges[i])
		}
	}
	chunks = make([]int64, 0, count)
	for i := 1; i < len(chunkRanges); i += 2 {
		for j := int64(0); j < (chunkRanges[i+1]); j++ {
			chunks = append(chunks, chunkRanges[i]+j*chunkSize)
		}
	}
	return
}

// GetLocalIPs returns all local ips
func GetLocalIPs() (ips []string, err error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		if ip := LocalIP(); ip != "" {
			return []string{ip}, nil
		}
		return
	}
	return localIPsFromAddrs(addrs), nil
}

func localIPsFromAddrs(addrs []net.Addr) (ips []string) {
	for _, address := range addrs {
		// Return every routable interface address. IPv6 link-local addresses are
		// discovered through multicast instead because dialing them also requires
		// the receiver's local interface zone.
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil || (ipnet.IP.To16() != nil && !ipnet.IP.IsLinkLocalUnicast()) {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return
}

func RandomFileName() (fname string, err error) {
	f, err := os.CreateTemp(".", "croc-stdin-")
	if err != nil {
		return
	}
	fname = f.Name()
	_ = f.Close()
	return
}

func FindOpenPorts(host string, portNumStart, numPorts int) (openPorts []int) {
	openPorts = []int{}
	for port := portNumStart; port-portNumStart < 200; port++ {
		timeout := 100 * time.Millisecond
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), timeout)
		if conn != nil {
			conn.Close()
		} else if err != nil {
			openPorts = append(openPorts, port)
		}
		if len(openPorts) >= numPorts {
			return
		}
	}
	return
}

// local ip determination
// https://stackoverflow.com/questions/41240761/check-if-ip-address-is-in-private-network-space
var privateIPBlocks []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // RFC3927 link-local
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local addr
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Errorf("parse error on %q: %v", cidr, err))
		}
		privateIPBlocks = append(privateIPBlocks, block)
	}
}

func IsLocalIP(ipaddress string) bool {
	if strings.Contains(ipaddress, "127.0.0.1") {
		return true
	}
	host, _, _ := net.SplitHostPort(ipaddress)
	ip := net.ParseIP(host)
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// ZipDirectory writes the contents of source into a zip at destination.
// Files whose absolute path is present in ignoredPaths are skipped (used by
// the --git flag to honour .gitignore rules). Files whose zip path contains
// any string in exclusions (case-insensitive) are also skipped, mirroring
// the post-walk filter in cli.go for non-zip transfers.
func ZipDirectory(destination string, source string, ignoredPaths map[string]bool, exclusions []string) (err error) {
	return ZipDirectoryWithExactExclusions(destination, source, ignoredPaths, exclusions, nil)
}

// ZipDirectoryWithExactExclusions is ZipDirectory with support for exact
// paths relative to source. Legacy exclusions remain case-insensitive
// substring matches.
func ZipDirectoryWithExactExclusions(destination string, source string, ignoredPaths map[string]bool, exclusions, exactExclusions []string) (err error) {
	if _, err = os.Stat(destination); err == nil {
		log.Errorf("%s file already exists!\n", destination)
		return fmt.Errorf("file already exists: %s", destination)
	}

	// Check if source directory exists
	if _, err := os.Stat(source); os.IsNotExist(err) {
		log.Errorf("Source directory does not exist: %s", source)
		return fmt.Errorf("source directory does not exist: %s", source)
	}

	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("failed to resolve source path: %w", err)
	}
	if resolvedSourceAbs, resolveErr := filepath.EvalSymlinks(sourceAbs); resolveErr == nil {
		sourceAbs = resolvedSourceAbs
	}
	destinationAbs, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("failed to resolve zip destination path: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Zipping %s to %s\n", source, destination)
	file, err := os.Create(destination)
	if err != nil {
		log.Error(err)
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer file.Close()
	if resolvedDestinationAbs, resolveErr := filepath.EvalSymlinks(destinationAbs); resolveErr == nil {
		destinationAbs = resolvedDestinationAbs
	}
	skipDestination := pathWithin(sourceAbs, destinationAbs)
	writer := zip.NewWriter(file)
	// no compression because croc does its compression on the fly
	writer.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.NoCompression)
	})
	defer writer.Close()

	// Get base name for zip structure
	baseName := strings.TrimSuffix(filepath.Base(destination), ".zip")

	// First pass: add the root directory with its modification time
	rootInfo, err := os.Stat(source)
	if err == nil && rootInfo.IsDir() {
		header, err := zip.FileInfoHeader(rootInfo)
		if err != nil {
			log.Error(err)
		} else {
			header.Name = baseName + "/" // Trailing slash indicates directory
			header.Method = zip.Store
			header.Modified = rootInfo.ModTime()

			_, err = writer.CreateHeader(header)
			if err != nil {
				log.Error(err)
			} else {
				fmt.Fprintf(os.Stderr, "\r\033[2K")
				fmt.Fprintf(os.Stderr, "\rAdding %s", baseName+"/")
			}
		}
	}

	// Second pass: add all other directories and files
	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Error(err)
			return nil
		}

		// Skip root directory (we already added it)
		if path == source {
			return nil
		}

		if skipDestination {
			absPath, absErr := filepath.Abs(path)
			if absErr == nil {
				if resolvedAbsPath, resolveErr := filepath.EvalSymlinks(absPath); resolveErr == nil {
					absPath = resolvedAbsPath
				}
				if filepath.Clean(absPath) == filepath.Clean(destinationAbs) {
					return nil
				}
			}
		}

		// Honour --git: skip paths flagged by .gitignore matching upstream.
		if len(ignoredPaths) > 0 {
			absPath, absErr := filepath.Abs(path)
			if absErr == nil && ignoredPaths[absPath] {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Calculate relative path from source directory
		relPath, err := filepath.Rel(source, path)
		if err != nil {
			log.Error(err)
			return nil
		}

		// Create zip path with base name structure
		zipPath := filepath.Join(baseName, relPath)
		zipPath = filepath.ToSlash(zipPath)
		relPath = NormalizeRelativePath(relPath)

		// Honour --exclude: case-insensitive substring match against the zip
		// path, mirroring the post-walk filter in cli.go.
		if len(exclusions) > 0 {
			zipPathLower := strings.ToLower(zipPath)
			for _, exclusion := range exclusions {
				if strings.Contains(zipPathLower, exclusion) {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
		}
		if len(exactExclusions) > 0 {
			for _, exclusion := range exactExclusions {
				if relPath == NormalizeRelativePath(exclusion) {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
		}

		if info.IsDir() {
			// Add directory entry to zip with original modification time
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				log.Error(err)
				return nil
			}
			header.Name = zipPath + "/" // Trailing slash indicates directory
			header.Method = zip.Store
			// Preserve the original modification time
			header.Modified = info.ModTime()

			_, err = writer.CreateHeader(header)
			if err != nil {
				log.Error(err)
				return nil
			}

			fmt.Fprintf(os.Stderr, "\r\033[2K")
			fmt.Fprintf(os.Stderr, "\rAdding %s", zipPath+"/")
			return nil
		}

		if info.Mode().IsRegular() {
			f1, err := os.Open(path)
			if err != nil {
				log.Error(err)
				return nil
			}
			defer f1.Close()

			// Create file header with modified time
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				log.Error(err)
				return nil
			}
			header.Name = zipPath
			header.Method = zip.Deflate

			w1, err := writer.CreateHeader(header)
			if err != nil {
				log.Error(err)
				return nil
			}

			if _, err := io.Copy(w1, f1); err != nil {
				log.Error(err)
				return nil
			}

			fmt.Fprintf(os.Stderr, "\r\033[2K")
			fmt.Fprintf(os.Stderr, "\rAdding %s", zipPath)
		}
		return nil
	})

	if err != nil {
		log.Error(err)
		return fmt.Errorf("error during directory walk: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\n")
	return nil
}

// NormalizeRelativePath converts a path to a portable, clean relative-path
// representation suitable for exact comparisons.
func NormalizeRelativePath(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	p = strings.TrimPrefix(p, "./")
	return p
}

func pathWithin(parent string, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(parent), filepath.Clean(child))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// RejectSymlinkPath rejects existing symlinks in target and its path
// components below root. This prevents writes through a pre-existing symlink
// from escaping the intended destination directory.
func RejectSymlinkPath(root, target string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to resolve destination root: %w", err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("failed to resolve destination path: %w", err)
	}

	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return fmt.Errorf("failed to resolve destination path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("destination path escapes root: '%s'", target)
	}
	if rel == "." {
		return nil
	}

	current := rootAbs
	for component := range strings.SplitSeq(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return fmt.Errorf("failed to inspect destination path '%s': %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to open symlink destination path component: '%s'", current)
		}
	}
	return nil
}

// ErrUnzipSizeLimit indicates that a zip archive would extract beyond the
// configured output-size limit.
var ErrUnzipSizeLimit = errors.New("zip extraction size limit exceeded")

// UnzipDirectory extracts source into destination. By default, extraction is
// limited to the archive's on-disk size. Croc-created zip archives use deflate
// without compression, so their extracted contents fit within this limit.
func UnzipDirectory(destination string, source string) error {
	archiveFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer archiveFile.Close()
	archiveInfo, err := archiveFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to inspect zip file: %w", err)
	}
	return unzipDirectoryFromReaderWithLimit(destination, nil, archiveFile, archiveInfo.Size(), archiveInfo.Size())
}

// UnzipDirectoryWithLimit extracts source into destination while allowing at
// most maxExtractedBytes bytes of regular-file output in total.
func UnzipDirectoryWithLimit(destination string, source string, maxExtractedBytes int64) error {
	if maxExtractedBytes <= 0 {
		return fmt.Errorf("maximum extracted size must be positive: %d", maxExtractedBytes)
	}
	archiveFile, err := os.Open(source)
	if err != nil {
		log.Error(err)
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer archiveFile.Close()
	archiveInfo, err := archiveFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to inspect zip file: %w", err)
	}
	return unzipDirectoryFromReaderWithLimit(destination, nil, archiveFile, archiveInfo.Size(), maxExtractedBytes)
}

// UnzipDirectoryFromFileAtRootWithLimit extracts through the caller's already
// opened receive root, so a renamed or replaced working-directory path cannot
// switch the extraction destination after the transfer began.
func UnzipDirectoryFromFileAtRootWithLimit(root *receivefs.Root, source *os.File, maxExtractedBytes int64) error {
	if root == nil {
		return errors.New("zip receive root is required")
	}
	if maxExtractedBytes <= 0 {
		return fmt.Errorf("maximum extracted size must be positive: %d", maxExtractedBytes)
	}
	archiveInfo, err := source.Stat()
	if err != nil {
		return fmt.Errorf("failed to inspect zip file: %w", err)
	}
	return unzipDirectoryFromReaderWithLimit("", root, source, archiveInfo.Size(), maxExtractedBytes)
}

func unzipDirectoryFromReaderWithLimit(
	destination string,
	receiveRoot *receivefs.Root,
	source io.ReaderAt,
	sourceSize int64,
	maxExtractedBytes int64,
) error {
	archive, err := zip.NewReader(source, sourceSize)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}

	normalized, err := validateZipEntries(archive.File, maxExtractedBytes)
	if err != nil {
		return err
	}

	root := receiveRoot
	if root == nil {
		if err = os.MkdirAll(destination, 0o755); err != nil {
			return fmt.Errorf("create zip destination: %w", err)
		}
		root, err = receivefs.OpenRoot(destination)
		if err != nil {
			return err
		}
		defer root.Close()
	}
	for _, entry := range normalized {
		if err = root.RejectSymlinkPath(entry.Path); err != nil {
			return fmt.Errorf("symlink destination path component in zip entry %q: %w", entry.Path, err)
		}
	}
	staging, err := root.CreateTempDir(".", ".croc-extract-")
	if err != nil {
		return fmt.Errorf("create zip staging directory: %w", err)
	}
	defer root.RemoveAll(staging)

	modTimes := make(map[string]time.Time)
	remainingExtractedBytes := maxExtractedBytes
	selected := make([]bool, len(archive.File))

	for i, f := range archive.File {
		filePath := normalized[i].Path
		fmt.Fprintf(os.Stderr, "\r\033[2K")
		fmt.Fprintf(os.Stderr, "\rUnzipping file %s", filePath)

		// Store modification time for this entry (BOTH files and directories)
		modifiedTime := f.Modified
		if modifiedTime.IsZero() {
			modifiedTime = f.FileHeader.Modified
		}
		if !modifiedTime.IsZero() {
			modTimes[filePath] = modifiedTime
		}

		if normalized[i].Kind == receivefs.KindDirectory {
			selected[i] = true
			continue
		}

		if _, statErr := root.Stat(filePath); statErr == nil {
			prompt := fmt.Sprintf("\nOverwrite '%s'? (y/N) ", filePath)
			choice, _ := GetInput(prompt)
			choice = strings.ToLower(choice)
			if choice != "y" && choice != "yes" {
				fmt.Fprintf(os.Stderr, "Skipping '%s'\n", filePath)
				continue
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect zip destination %q: %w", filePath, statErr)
		}
		selected[i] = true

		fileInArchive, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %q: %w", f.Name, err)
		}
		stagedPath := path.Join(staging, filePath)
		if err = root.MkdirAll(path.Dir(stagedPath), 0o700); err != nil {
			fileInArchive.Close()
			return fmt.Errorf("create staging directory for zip entry %q: %w", f.Name, err)
		}
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o600
		}
		dstFile, err := root.OpenFile(stagedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			fileInArchive.Close()
			return fmt.Errorf("create staging file for zip entry %q: %w", f.Name, err)
		}

		_, copyErr := copyWithExtractedSizeLimit(dstFile, fileInArchive, &remainingExtractedBytes)
		archiveCloseErr := fileInArchive.Close()
		if copyErr == nil {
			copyErr = dstFile.Sync()
		}
		destinationCloseErr := dstFile.Close()
		if copyErr != nil {
			return fmt.Errorf("failed to extract zip entry %q: %w", f.Name, copyErr)
		}
		if archiveCloseErr != nil {
			return fmt.Errorf("failed to close zip entry %q: %w", f.Name, archiveCloseErr)
		}
		if destinationCloseErr != nil {
			return fmt.Errorf("failed to close extracted file for zip entry %q: %w", f.Name, destinationCloseErr)
		}
		if modTime, ok := modTimes[filePath]; ok {
			if err = root.Chtimes(stagedPath, modTime, modTime); err != nil {
				return fmt.Errorf("set staged modification time for zip entry %q: %w", f.Name, err)
			}
		}
	}

	// Commit directories before files, always through the opened root.
	for i, entry := range normalized {
		if selected[i] && entry.Kind == receivefs.KindDirectory {
			if err = root.MkdirAll(entry.Path, 0o755); err != nil {
				return fmt.Errorf("commit zip directory %q: %w", entry.Path, err)
			}
		}
	}
	for i, entry := range normalized {
		if !selected[i] || entry.Kind != receivefs.KindFile {
			continue
		}
		if err = root.MkdirAll(path.Dir(entry.Path), 0o755); err != nil {
			return fmt.Errorf("commit parent for zip entry %q: %w", entry.Path, err)
		}
		if err = root.Rename(path.Join(staging, entry.Path), entry.Path); err != nil {
			return fmt.Errorf("commit zip entry %q: %w", entry.Path, err)
		}
	}
	for i, entry := range normalized {
		if !selected[i] || entry.Kind != receivefs.KindDirectory {
			continue
		}
		if modTime, ok := modTimes[entry.Path]; ok {
			if err = root.Chtimes(entry.Path, modTime, modTime); err != nil {
				return fmt.Errorf("set modification time for zip directory %q: %w", entry.Path, err)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\n")
	return nil
}

func validateZipEntries(files []*zip.File, maxExtractedBytes int64) ([]receivefs.Entry, error) {
	if maxExtractedBytes <= 0 {
		return nil, fmt.Errorf("maximum extracted size must be positive: %d", maxExtractedBytes)
	}
	entries := make([]receivefs.Entry, len(files))
	remainingDeclaredBytes := uint64(maxExtractedBytes)
	for i, f := range files {
		kind := receivefs.KindFile
		if f.FileInfo().IsDir() {
			kind = receivefs.KindDirectory
		} else if !f.Mode().IsRegular() {
			return nil, fmt.Errorf("unsupported zip entry type for %q", f.Name)
		}
		entries[i] = receivefs.Entry{Path: f.Name, Kind: kind}

		if kind == receivefs.KindFile {
			if f.UncompressedSize64 > remainingDeclaredBytes {
				return nil, fmt.Errorf(
					"zip entry %q declares %d extracted bytes with %d bytes remaining: %w",
					f.Name,
					f.UncompressedSize64,
					remainingDeclaredBytes,
					ErrUnzipSizeLimit,
				)
			}
			remainingDeclaredBytes -= f.UncompressedSize64
		}
	}
	normalized, err := receivefs.ValidateEntries(entries)
	if err != nil {
		return nil, fmt.Errorf("invalid file path in zip entry: %w", err)
	}
	return normalized, nil
}

func copyWithExtractedSizeLimit(destination io.Writer, source io.Reader, remainingBytes *int64) (int64, error) {
	limitedSource := &io.LimitedReader{R: source, N: *remainingBytes}
	written, err := io.Copy(destination, limitedSource)
	*remainingBytes -= written
	if err != nil {
		return written, err
	}
	if limitedSource.N != 0 {
		return written, nil
	}

	var probe [1]byte
	probeBytes, probeErr := io.ReadFull(source, probe[:])
	if probeBytes > 0 {
		return written, ErrUnzipSizeLimit
	}
	if probeErr != nil && !errors.Is(probeErr, io.EOF) && !errors.Is(probeErr, io.ErrUnexpectedEOF) {
		return written, probeErr
	}
	return written, nil
}

func resolveUnzipPath(destination string, entryName string) (string, error) {
	if !filepath.IsLocal(entryName) {
		return "", fmt.Errorf("path escapes destination")
	}

	destinationAbs, err := filepath.Abs(destination)
	if err != nil {
		return "", fmt.Errorf("failed to resolve destination: %w", err)
	}
	destinationAbs = filepath.Clean(destinationAbs)

	filePath := filepath.Clean(filepath.Join(destinationAbs, entryName))
	relativePath, err := filepath.Rel(destinationAbs, filePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %q: %w", entryName, err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes destination")
	}

	return filePath, nil
}

// ValidFileName checks if a filename is valid
// by making sure it has no invisible characters
func ValidFileName(fname string) (err error) {
	// make sure it doesn't contain unicode or invisible characters
	for _, r := range fname {
		if !unicode.IsGraphic(r) {
			err = fmt.Errorf("non-graphical unicode: %x U+%d in '%x'", string(r), r, fname)
			return
		}
		if !unicode.IsPrint(r) {
			err = fmt.Errorf("non-printable unicode: %x U+%d in '%x'", string(r), r, fname)
			return
		}
	}
	// make sure basename does not include path separators
	_, basename := filepath.Split(fname)
	if strings.Contains(basename, string(os.PathSeparator)) {
		err = fmt.Errorf("basename cannot contain path separators: '%s'", basename)
		return
	}
	// make sure the filename is not an absolute path
	if filepath.IsAbs(fname) {
		err = fmt.Errorf("filename cannot be an absolute path: '%s'", fname)
		return
	}
	if !filepath.IsLocal(fname) {
		err = fmt.Errorf("filename must be a local path: '%s'", fname)
		return
	}
	return
}

var markedFiles = struct {
	sync.Mutex
	paths []string
}{}

func MarkFileForRemoval(fname string) {
	// Cleanup is only for temporary files croc creates in its working directory.
	// Keep the list in memory so a received file cannot replace it and choose
	// arbitrary paths for deletion.
	if !filepath.IsLocal(fname) {
		log.Debugf("refusing to mark non-local path for removal: %q", fname)
		return
	}
	fname, err := filepath.Abs(fname)
	if err != nil {
		log.Debug(err)
		return
	}

	markedFiles.Lock()
	markedFiles.paths = append(markedFiles.paths, fname)
	markedFiles.Unlock()
}

func RemoveMarkedFiles() (err error) {
	markedFiles.Lock()
	paths := markedFiles.paths
	markedFiles.paths = nil
	markedFiles.Unlock()

	for _, fname := range paths {
		removeErr := os.Remove(fname)
		if removeErr == nil {
			log.Tracef("Removed %s", fname)
		} else if !os.IsNotExist(removeErr) {
			err = errors.Join(err, removeErr)
		}
	}
	return
}
