package tarinator

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestArchive(t *testing.T) {
	paths := []string{
		"somescript.sh",
		"./test_files/",
	}

	err := Tarinate(paths, "output_test.tar.gz")
	if err != nil {
		t.Errorf("Failed: %s\n", err)
		return
	}
}

func TestOpen(t *testing.T) {
	if _, err := os.Stat("output_test.tar.gz"); os.IsNotExist(err) {
		t.Error("No file for untaring dected")
		return
	}
	err := os.Mkdir("testing", 0755)
	if err != nil {
		os.RemoveAll("testing")
		os.Mkdir("testing", 0755)
	}

	err = UnTarinate("testing", "output_test.tar.gz")
	if err != nil {
		t.Errorf("Failed untaring: %s\n", err)
		return
	}

	files, err := ioutil.ReadDir("testing")
	if err != nil {
		t.Error(err)
		return
	}

	if len(files) != 2 {
		t.Errorf("The directory wasn't actually written")
	}
}

func isDir(pth string) (bool, error) {
	fi, err := os.Stat(pth)
	if err != nil {
		return false, err
	}

	return fi.IsDir(), nil
}
