package zipper

import (
	"archive/zip"
	"compress/flate"
	"io"
	"os"
	"path"
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
	log.Debugf("zipping %s with compression? %v", fname, compress)

	// get absolute filename
	fname, err = filepath.Abs(fname)
	if err != nil {
		log.Error(err)
		return
	}

	// get path to file and the filename
	fpath, fname := filepath.Split(fname)

	writtenFilename = fname + ".croc.zip"
	log.Debugf("creating file: %s", writtenFilename)
	f, err := os.Create(writtenFilename)
	if err != nil {
		log.Error(err)
		return
	}

	zipWriter := zip.NewWriter(f)
	zipWriter.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		if compress {
			return flate.NewWriter(out, flate.BestSpeed)
		} else {
			return flate.NewWriter(out, flate.NoCompression)
		}
	})
	defer zipWriter.Close()

	// Get the file information for the target
	log.Debugf("checking %s", path.Join(fpath, fname))
	ftarget, err := os.Open(path.Join(fpath, fname))
	if err != nil {
		log.Error(err)
		return
	}
	defer ftarget.Close()
	info, err := ftarget.Stat()
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
		baseDir := path.Join(fpath, fname)
		log.Debugf("walking base dir: %s", baseDir)
		filepath.Walk(baseDir, func(curpath string, info os.FileInfo, err error) error {
			if err != nil {
				log.Error(err)
				return err
			}

			header, err := zip.FileInfoHeader(info)
			if err != nil {
				log.Error(err)
				return err
			}

			if baseDir != "" {
				header.Name = path.Join(fname, strings.TrimPrefix(curpath, baseDir))
			}
			log.Debug(header.Name)

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

			file, err := os.Open(curpath)
			if err != nil {
				log.Error(err)
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
		_, err = io.Copy(writer, ftarget)
		if err != nil {
			log.Error(err)
			return
		}
	}

	log.Debugf("wrote zip file to %s", writtenFilename)
	return
}
