package croc

import (
	"archive/zip"
	"compress/flate"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	log "github.com/cihub/seelog"
)

func unzipFile(src, dest string) (err error) {
	r, err := zip.OpenReader(src)
	if err != nil {
		return
	}
	defer r.Close()

	for _, f := range r.File {
		var rc io.ReadCloser
		rc, err = f.Open()
		if err != nil {
			return
		}
		defer rc.Close()

		// Store filename/path for returning and using later on
		fpath := filepath.Join(dest, f.Name)
		log.Debugf("unzipping %s", fpath)

		if f.FileInfo().IsDir() {

			// Make Folder
			os.MkdirAll(fpath, os.ModePerm)

		} else {

			// Make File
			if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
				return
			}

			var outFile *os.File
			outFile, err = os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return
			}

			_, err = io.Copy(outFile, rc)

			// Close the file without defer to close before next iteration of loop
			outFile.Close()

			if err != nil {
				return
			}

		}
	}
	if err == nil {
		log.Debugf("unzipped %s to %s", src, dest)
	}
	return
}

func zipFile(fname string, compress bool) (writtenFilename string, err error) {
	pathtofile, filename := filepath.Split(fname)
	curdir, err := os.Getwd()
	if err != nil {
		log.Error(err)
		return
	}
	curdir, err = filepath.Abs(curdir)
	if err != nil {
		log.Error(err)
		return
	}
	defer os.Chdir(curdir)
	os.Chdir(pathtofile)

	newfile, err := ioutil.TempFile("/tmp/", "croc")
	if err != nil {
		log.Error(err)
		return
	}
	writtenFilename = newfile.Name()
	defer newfile.Close()

	zipWriter := zip.NewWriter(newfile)
	zipWriter.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		if compress {
			return flate.NewWriter(out, flate.BestCompression)
		} else {
			return flate.NewWriter(out, flate.NoCompression)
		}
	})
	defer zipWriter.Close()

	zipfile, err := os.Open(filename)
	if err != nil {
		log.Error(err)
		return "", err
	}
	defer zipfile.Close()
	// Get the file information
	info, err := zipfile.Stat()
	if err != nil {
		log.Error(err)
		return
	}

	// write header informaiton
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		log.Error(err)
		return
	}

	var writer io.Writer
	if info.IsDir() {
		baseDir := filename
		filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}

			if baseDir != "" {
				header.Name = filepath.Join(baseDir, strings.TrimPrefix(path, baseDir))
			}

			if info.IsDir() {
				header.Name += "/"
			} else {
				header.Method = zip.Deflate
			}

			writer, err = zipWriter.CreateHeader(header)
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(writer, file)
			return err
		})
	} else {
		writer, err = zipWriter.CreateHeader(header)
		if err != nil {
			log.Error(err)
			return
		}
		_, err = io.Copy(writer, newfile)
		if err != nil {
			log.Error(err)
			return
		}
	}

	log.Debugf("wrote zip file to %s", writtenFilename)
	return
}
