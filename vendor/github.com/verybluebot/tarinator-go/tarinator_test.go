package tarinator

import(
    "testing"
    "os"
)

func TestTarGzFromFiles(t *testing.T) {
    paths := []string{
        "somescript.sh",
        "test_files/",
    }

    err := Tarinate(paths, "output_test.tar.gz")
    if err != nil {
        t.Errorf("Failed: %s\n", err)
        return
    }
}

func TestUnTarGzFromFiles(t *testing.T) {
    if _, err := os.Stat("output_test.tar.gz"); os.IsNotExist(err) {
        t.Error("No file for untaring dected")
        return
    }

    err := UnTarinate("/tmp", "output_test.tar.gz")
    if err != nil {
        t.Errorf("Failed untaring: %s\n", err)
        return
    }
}





