package croc

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/schollz/croc/v9/src/tcp"
	log "github.com/schollz/logger"
	"github.com/stretchr/testify/assert"
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
	go func() {
		filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{"../../README.md"}, false)
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo, emptyFolders, totalNumberFolders)
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

func TestCrocEmptyFolder(t *testing.T) {
	pathName := "../../testEmpty"
	defer os.RemoveAll(pathName)
	defer os.RemoveAll("./testEmpty")
	os.MkdirAll(pathName, 0755)

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
	go func() {
		filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{pathName}, false)
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo, emptyFolders, totalNumberFolders)
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
	defer os.RemoveAll("./link-in-folder")
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
		filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{pathName}, false)
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo, emptyFolders, totalNumberFolders)
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
	if s != "../../README.md" && s != "..\\..\\README.md" {
		log.Debug(s)
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
		filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{"../../LICENSE", "touched"}, false)
		if errGet != nil {
			t.Errorf("failed to get minimal info: %v", errGet)
		}
		err := sender.Send(filesInfo, emptyFolders, totalNumberFolders)
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
	filesInfo, emptyFolders, totalNumberFolders, errGet := GetFilesInfo([]string{tmpfile.Name()}, false)
	if errGet != nil {
		t.Errorf("failed to get minimal info: %v", errGet)
	}
	err = sender.Send(filesInfo, emptyFolders, totalNumberFolders)
	log.Debug(err)
	assert.NotNil(t, err)
}

func TestCleanUp(t *testing.T) {
	// windows allows files to be deleted only if they
	// are not open by another program so the remove actions
	// from the above tests will not always do a good clean up
	// This "test" will make sure
	operatingSystem := runtime.GOOS
	log.Debugf("The operating system is %s", operatingSystem)
	if operatingSystem == "windows" {
		time.Sleep(1 * time.Second)
		log.Debug("Full cleanup")
		var err error

		for _, file := range []string{"README.md", "./README.md"} {
			err = os.Remove(file)
			if err == nil {
				log.Debugf("Successfuly purged %s", file)
			} else {
				log.Debugf("%s was already purged.", file)
			}
		}
		for _, folder := range []string{"./testEmpty", "./link-in-folder"} {
			err = os.RemoveAll(folder)
			if err == nil {
				log.Debugf("Successfuly purged %s", folder)
			} else {
				log.Debugf("%s was already purged.", folder)
			}
		}
	}
}
