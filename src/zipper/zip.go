package zipper

import (
	"archive/zip"
	"compress/flate"
	"io"
	"os"
	"path/filepath"
	"strings"

	log "github.com/cihub/seelog"
	"github.com/schollz/croc/src/logger"
)

var DebugLevel string

func init() {
	DebugLevel = "info"
}

// UnzipFile will unzip the src directory into the dest
func UnzipFile(src, dest string) (err error) {
	logger.SetLogLevel(DebugLevel)

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
		fpath = filepath.FromSlash(fpath)

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

// ZipFile will zip the folder
func ZipFile(fname string, compress bool) (writtenFilename string, err error) {
	logger.SetLogLevel(DebugLevel)
	fname, err = filepath.Abs(fname)
	if err != nil {
		return
	}
	log.Debugf("zipping %s with compression? %v", fname, compress)
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
	log.Debugf("current directory: %s", curdir)
	newfile, err := os.Create(fname + ".zip")
	if err != nil {
		log.Error(err)
		return
	}
	_, writtenFilename = filepath.Split(newfile.Name())
	defer newfile.Close()

	defer os.Chdir(curdir)
	log.Debugf("changing dir to %s", pathtofile)
	os.Chdir(pathtofile)

	zipWriter := zip.NewWriter(newfile)
	zipWriter.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		if compress {
			return flate.NewWriter(out, flate.BestSpeed)
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

			header.Name = filepath.ToSlash(header.Name)

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
		_, err = io.Copy(writer, zipfile)
		if err != nil {
			log.Error(err)
			return
		}
	}

	log.Debugf("wrote zip file to %s", writtenFilename)
	return
}
