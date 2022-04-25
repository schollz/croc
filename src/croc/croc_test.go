package croc

import (
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v9/src/tcp"
	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
	"net/http" // Added by Mawoka to run the http-request
	"github.com/buger/jsonparser" // Added by Mawoka for github-api-parsing
	"fmt" // Added by Mawoka for github-api-parsing
)

func init() {
	log.SetLevel("trace")

	go tcp.Run("debug", "localhost", "8281", "pass123", "8282,8283,8284,8285")
	go tcp.Run("debug", "localhost", "8282", "pass123")
	go tcp.Run("debug", "localhost", "8283", "pass123")
	go tcp.Run("debug", "localhost", "8284", "pass123")
	go tcp.Run("debug", "localhost", "8285", "pass123")
	time.Sleep(1 * time.Second)
}

func TestCrocReadme(t *testing.T) {
	defer os.Remove("README.md")

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8281",
		RelayPorts:    []string{"8281"},
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8281",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	http_resp, _ := http.Get("https://api.github.com/repos/schollz/croc/releases/latest")
	body, _ := ioutil.ReadAll(http_resp.Body)
	gh_version, _ := jsonparser.GetString([]byte(fmt.Sprintf("%s\n", body)), "name")
	go func() {
		filesInfo, errGet := GetFilesInfo([]string{"../../README.md"})
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo, gh_version)
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		err := receiver.Receive()
		if err != nil {
			t.Errorf("receive failed: %v", err)
		}
		wg.Done()
	}()

	wg.Wait()
}

func TestCrocSymlink(t *testing.T) {
	pathName := "../link-in-folder"
	defer os.RemoveAll(pathName)
	os.MkdirAll(pathName, 0755)
	os.Symlink("../../README.md", filepath.Join(pathName, "README.link"))

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  "8124-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8281",
		RelayPorts:    []string{"8281"},
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8124-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8281",
		RelayPassword: "pass123",
		Stdout:        false,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		filesInfo, errGet := GetFilesInfo([]string{pathName})
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo)
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		err := receiver.Receive()
		if err != nil {
			t.Errorf("receive failed: %v", err)
		}
		wg.Done()
	}()

	wg.Wait()

	s, err := filepath.EvalSymlinks(path.Join(pathName, "README.link"))
	if s != "../../README.md" {
		t.Errorf("symlink failed to transfer in folder")
	}
	if err != nil {
		t.Errorf("symlink transfer failed: %s", err.Error())
	}
}

func TestCrocLocal(t *testing.T) {
	log.SetLevel("trace")
	defer os.Remove("LICENSE")
	defer os.Remove("touched")
	time.Sleep(300 * time.Millisecond)

	log.Debug("setting up sender")
	sender, err := New(Options{
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8181",
		RelayPorts:    []string{"8181", "8182"},
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}
	time.Sleep(1 * time.Second)

	log.Debug("setting up receiver")
	receiver, err := New(Options{
		IsSender:      false,
		SharedSecret:  "8123-testingthecroc",
		Debug:         true,
		RelayAddress:  "localhost:8181",
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  false,
		Curve:         "siec",
		Overwrite:     true,
	})
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	os.Create("touched")
	wg.Add(2)
	go func() {
		http_resp, _ := http.Get("https://api.github.com/repos/schollz/croc/releases/latest")
		body, _ := ioutil.ReadAll(http_resp.Body)
		gh_version, _ := jsonparser.GetString([]byte(fmt.Sprintf("%s\n", body)), "name")
		err = sender.Send(TransferOptions{
			PathToFiles:      []string{"../../LICENSE", "touched"},
			KeepPathInRemote: false,
		}, gh_version)

		filesInfo, errGet := GetFilesInfo([]string{"../../LICENSE", "touched"})
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo, gh_version)
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()
	time.Sleep(100 * time.Millisecond)
	go func() {
		err := receiver.Receive()
		if err != nil {
			t.Errorf("send failed: %v", err)
		}
		wg.Done()
	}()

	wg.Wait()
}

func TestCrocError(t *testing.T) {
	content := []byte("temporary file's content")
	tmpfile, err := os.CreateTemp("", "example")
	if err != nil {
		panic(err)
	}

	defer os.Remove(tmpfile.Name()) // clean up

	if _, err := tmpfile.Write(content); err != nil {
		panic(err)
	}
	if err := tmpfile.Close(); err != nil {
		panic(err)
	}

	Debug(false)
	log.SetLevel("warn")
	sender, _ := New(Options{
		IsSender:      true,
		SharedSecret:  "8123-testingthecroc2",
		Debug:         true,
		RelayAddress:  "doesntexistok.com:8381",
		RelayPorts:    []string{"8381", "8382"},
		RelayPassword: "pass123",
		Stdout:        true,
		NoPrompt:      true,
		DisableLocal:  true,
		Curve:         "siec",
		Overwrite:     true,
	})
	http_resp, _ := http.Get("https://api.github.com/repos/schollz/croc/releases/latest")
	body, _ := ioutil.ReadAll(http_resp.Body)
	gh_version, _ := jsonparser.GetString([]byte(fmt.Sprintf("%s\n", body)), "name")
	filesInfo, errGet := GetFilesInfo([]string{tmpfile.Name()})
	if errGet != nil {
		t.Errorf("failed to get minimal info: %v", errGet)
	}
	err = sender.Send(filesInfo, gh_version)
	log.Debug(err)
	assert.NotNil(t, err)

}
