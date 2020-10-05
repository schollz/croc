package utils

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cespare/xxhash"
	"github.com/kalafut/imohash"
	"github.com/schollz/mnemonicode"
)

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

// HashFile returns the hash of a file
func HashFile(fname string) (hash256 []byte, err error) {
	return IMOHashFile(fname)
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
		bodyBytes, err := ioutil.ReadAll(resp.Body)
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

// GetRandomName returns mnemoicoded random name
func GetRandomName() string {
	var result []string
	bs := make([]byte, 4)
	rand.Read(bs)
	result = mnemonicode.EncodeWordList(result, bs)
	return strings.Join(result, "-")
}

// ByteCountDecimal converts bytes to human readable byte string
func ByteCountDecimal(b int64) string {
	const unit = 1000
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
		chunks = chunkRanges
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
	f, err := ioutil.TempFile(".", "croc-stdin-")
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
