package tarinator

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func Tarinate(paths []string, tarPath string) error {
	file, err := os.Create(tarPath)
	if err != nil {
		return err
	}

	defer file.Close()

	var fileReader io.WriteCloser = file

	if strings.HasSuffix(tarPath, ".gz") {
		fileReader = gzip.NewWriter(file)

		defer fileReader.Close()
	}

	tw := tar.NewWriter(fileReader)
	defer tw.Close()

	for _, i := range paths {
		if err := tarwalk(i, "", tw); err != nil {
			return err
		}
	}

	return nil
}

func tarwalk(source, target string, tw *tar.Writer) error {
	info, err := os.Stat(source)
	if err != nil {
		return nil
	}

	var baseDir string
	if info.IsDir() {
		baseDir = filepath.Base(source)
	}

	return filepath.Walk(source,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			header, err := tar.FileInfoHeader(info, info.Name())
			if err != nil {
				return err
			}

			if baseDir != "" {
				header.Name = filepath.ToSlash(filepath.Join(baseDir, strings.TrimPrefix(path, source)))
			}

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			if !info.Mode().IsRegular() {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(tw, file)
			return err
		})
}

func UnTarinate(extractPath, sourcefile string) error {
	file, err := os.Open(sourcefile)

	if err != nil {
		return err
	}

	defer file.Close()

	var fileReader io.ReadCloser = file

	if strings.HasSuffix(sourcefile, ".gz") {
		if fileReader, err = gzip.NewReader(file); err != nil {
			return err
		}
		defer fileReader.Close()
	}

	tarBallReader := tar.NewReader(fileReader)

	for {
		header, err := tarBallReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		filename := filepath.Join(extractPath, filepath.FromSlash(header.Name))

		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(filename, os.FileMode(header.Mode)) // or use 0755 if you prefer

			if err != nil {
				return err
			}

		case tar.TypeReg:
			writer, err := os.Create(filename)

			if err != nil {
				return err
			}

			io.Copy(writer, tarBallReader)

			err = os.Chmod(filename, os.FileMode(header.Mode))

			if err != nil {
				return err
			}

			writer.Close()
		default:
			log.Printf("Unable to untar type: %c in file %s", header.Typeflag, filename)
		}
	}
	return nil
}
