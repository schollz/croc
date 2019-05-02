package utils

import (
	"bufio"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"bytes"
	"net"
	"math"
	"net/http"
	"os"
	"strings"

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

// HashFile returns the md5 hash of a file
func HashFile(fname string) (hash256 []byte, err error) {
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

// SHA256 returns sha256 sum
func SHA256(s string) string {
	sha := sha256.New()
	sha.Write([]byte(s))
	return fmt.Sprintf("%x", sha.Sum(nil))
}

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

// Get preferred outbound ip of this machine
func LocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP.String()
}

func GetRandomName() string {
	result := []string{}
	bs := make([]byte, 4)
	rand.Read(bs)
	result = mnemonicode.EncodeWordList(result, bs)
	return strings.Join(result, "-")
}

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
func MissingChunks(fname string, fsize int64, chunkSize int) (chunks []int64) {
	fstat, err := os.Stat(fname)
	if fstat.Size() != fsize || err != nil {
		return
	}

	f, err := os.Open(fname)
	if err != nil {
		return
	}
	defer f.Close()

	emptyBuffer := make([]byte, chunkSize)
	chunkNum := 0
	chunks = make([]int64, int64(math.Ceil(float64(fsize)/float64(chunkSize))))
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
		chunks = []int64{}
	} else {
		chunks = chunks[:chunkNum]
	}
	return
}
