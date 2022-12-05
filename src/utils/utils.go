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
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cespare/xxhash"
	"github.com/kalafut/imohash"
	"github.com/schollz/mnemonicode"
)

// Get or create home directory
func GetConfigDir() (homedir string, err error) {
	homedir, err = os.UserHomeDir()
	if err != nil {
		return
	}

	if envHomedir, isSet := os.LookupEnv("CROC_CONFIG_DIR"); isSet {
		homedir = envHomedir
	} else if xdgConfigHome, isSet := os.LookupEnv("XDG_CONFIG_HOME"); isSet {
		homedir = path.Join(xdgConfigHome, "croc")
	} else {
		homedir = path.Join(homedir, ".config", "croc")
	}

	if _, err = os.Stat(homedir); os.IsNotExist(err) {
		err = os.MkdirAll(homedir, 0o700)
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

// GetInput returns the input with a given prompt
func GetInput(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintf(os.Stderr, "%s", prompt)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

// HashFile returns the hash of a file or, in case of a symlink, the
// SHA256 hash of its target. Takes an argument to specify the algorithm to use.
func HashFile(fname string, algorithm string) (hash256 []byte, err error) {
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
	switch algorithm {
	case "imohash":
		return IMOHashFile(fname)
	case "md5":
		return MD5HashFile(fname)
	case "xxhash":
		return XXHashFile(fname)
	}
	err = fmt.Errorf("unspecified algorithm")
	return
}

// MD5HashFile returns MD5 hash
func MD5HashFile(fname string) (hash256 []byte, err error) {
	f, err := os.Open(fname)
	if err != nil {
		return
	}
	defer f.Close()

	h := md5.New()
	if _, err = io.Copy(h, f); err != nil {
		return
	}

	hash256 = h.Sum(nil)
	return
}

// IMOHashFile returns imohash
func IMOHashFile(fname string) (hash []byte, err error) {
	b, err := imohash.SumFile(fname)
	hash = b[:]
	return
}

var imofull = imohash.NewCustom(0, 0)

// IMOHashFileFull returns imohash of full file
func IMOHashFileFull(fname string) (hash []byte, err error) {
	b, err := imofull.SumFile(fname)
	hash = b[:]
	return
}

// XXHashFile returns the xxhash of a file
func XXHashFile(fname string) (hash256 []byte, err error) {
	f, err := os.Open(fname)
	if err != nil {
		return
	}
	defer f.Close()

	h := xxhash.New()
	if _, err = io.Copy(h, f); err != nil {
		return
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
	resp, err := http.Get("https://canhazip.com")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		ip = strings.TrimSpace(string(bodyBytes))
	}
	return
}

// LocalIP returns local ip address
func LocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP.String()
}

func GenerateRandomPin() string {
	s := ""
	max := new(big.Int)
	max.SetInt64(9)
	for i := 0; i < 4; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		s += fmt.Sprintf("%d", v)
	}
	return s
}

// GetRandomName returns mnemonicoded random name
func GetRandomName() string {
	var result []string
	bs := make([]byte, 4)
	rand.Read(bs)
	result = mnemonicode.EncodeWordList(result, bs)
	return GenerateRandomPin() + "-" + strings.Join(result, "-")
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

	emptyBuffer := make([]byte, chunkSize)
	chunkNum := 0
	chunks := make([]int64, int64(math.Ceil(float64(fsize)/float64(chunkSize))))
	var currentLocation int64
	for {
		buffer := make([]byte, chunkSize)
		bytesread, err := f.Read(buffer)
		if err != nil {
			break
		}
		if bytes.Equal(buffer[:bytesread], emptyBuffer[:bytesread]) {
			chunks[chunkNum] = currentLocation
			chunkNum++
		}
		currentLocation += int64(bytesread)
	}
	if chunkNum == 0 {
		chunkRanges = []int64{}
	} else {
		chunks = chunks[:chunkNum]
		chunkRanges = []int64{int64(chunkSize), chunks[0]}
		curCount := 0
		for i, chunk := range chunks {
			if i == 0 {
				continue
			}
			curCount++
			if chunk-chunks[i-1] > int64(chunkSize) {
				chunkRanges = append(chunkRanges, int64(curCount))
				chunkRanges = append(chunkRanges, chunk)
				curCount = 0
			}
		}
		chunkRanges = append(chunkRanges, int64(curCount+1))
	}
	return
}

// ChunkRangesToChunks converts chunk ranges to list
func ChunkRangesToChunks(chunkRanges []int64) (chunks []int64) {
	if len(chunkRanges) == 0 {
		return
	}
	chunkSize := chunkRanges[0]
	chunks = []int64{}
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
		return
	}
	ips = []string{}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
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
	if strings.Contains(ipaddress, "localhost") {
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

func ZipDirectory(destination string, source string) (err error) {
	if _, err = os.Stat(destination); err == nil {
		log.Fatalf("%s file already exists!\n", destination)
	}
	fmt.Fprintf(os.Stderr, "Zipping %s to %s\n", source, destination)
	file, err := os.Create(destination)
	if err != nil {
		log.Fatalln(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	// no compression because croc does its compression on the fly
	writer.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.NoCompression)
	})
	defer writer.Close()
	err = filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Fatalln(err)
		}
		if info.Mode().IsRegular() {
			f1, err := os.Open(path)
			if err != nil {
				log.Fatalln(err)
			}
			defer f1.Close()
			zipPath := strings.ReplaceAll(path, source, strings.TrimSuffix(destination, ".zip"))
			w1, err := writer.Create(zipPath)
			if err != nil {
				log.Fatalln(err)
			}
			if _, err := io.Copy(w1, f1); err != nil {
				log.Fatalln(err)
			}
			fmt.Fprintf(os.Stderr, "\r\033[2K")
			fmt.Fprintf(os.Stderr, "\rAdding %s", zipPath)
		}
		return nil
	})
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Fprintf(os.Stderr, "\n")
	return nil
}

func UnzipDirectory(destination string, source string) error {
	archive, err := zip.OpenReader(source)
	if err != nil {
		log.Fatalln(err)
	}
	defer archive.Close()

	for _, f := range archive.File {
		filePath := filepath.Join(destination, f.Name)
		fmt.Fprintf(os.Stderr, "\r\033[2K")
		fmt.Fprintf(os.Stderr, "\rUnzipping file %s", filePath)
		if f.FileInfo().IsDir() {
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			log.Fatalln(err)
		}

		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			log.Fatalln(err)
		}

		fileInArchive, err := f.Open()
		if err != nil {
			log.Fatalln(err)
		}

		if _, err := io.Copy(dstFile, fileInArchive); err != nil {
			log.Fatalln(err)
		}

		dstFile.Close()
		fileInArchive.Close()
	}
	fmt.Fprintf(os.Stderr, "\n")
	return nil
}
