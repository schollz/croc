package zipper

import (
	"archive/zip"
	"compress/flate"
	"fmt"
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

// ZipFiles will zip all the files and the folders as if they were in the same directory
func ZipFiles(fnames []string, compress bool) (writtenFilename string, err error) {
	logger.SetLogLevel(DebugLevel)
	if len(fnames) == 0 {
		err = fmt.Errorf("must provide files to zip")
		return
	}

	log.Debugf("zipping %s with compression? %v", fnames, compress)
	writtenFilename = fmt.Sprintf("%d_files.croc.zip", len(fnames))
	err = makeZip(writtenFilename, fnames, compress)
	return
}

// ZipFile will zip the folder
func ZipFile(fname string, compress bool) (writtenFilename string, err error) {
	logger.SetLogLevel(DebugLevel)

	// get path to file and the filename
	_, filename := filepath.Split(fname)
	writtenFilename = filename + ".croc.zip"
	err = makeZip(writtenFilename, []string{fname}, compress)
	return
}

func makeZip(writtenFilename string, fnames []string, compress bool) (err error) {
	log.Debugf("creating file: %s", writtenFilename)
	f, err := os.Create(writtenFilename)
	if err != nil {
		log.Error(err)
		return
	}
	defer f.Close()

	zipWriter := zip.NewWriter(f)
	zipWriter.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		if compress {
			return flate.NewWriter(out, flate.BestSpeed)
		} else {
			return flate.NewWriter(out, flate.NoCompression)
		}
	})
	defer zipWriter.Close()

	err = zipFiles(fnames, compress, zipWriter)
	if err == nil {
		log.Debugf("wrote zip file to %s", writtenFilename)
	} else {
		log.Error(err)
	}
	return
}

func zipFiles(fnames []string, compress bool, zipWriter *zip.Writer) (err error) {
	for _, fname := range fnames {
		// get absolute filename
		absPath, err := filepath.Abs(filepath.Clean(fname))
		if err != nil {
			log.Error(err)
			return err
		}
		absPath = filepath.ToSlash(absPath)

		// get path to file and the filename
		fpath, fname := filepath.Split(absPath)

		// Get the file information for the target
		log.Debugf("checking %s", absPath)
		ftarget, err := os.Open(absPath)
		if err != nil {
			log.Error(err)
			return err
		}
		defer ftarget.Close()
		info, err := ftarget.Stat()
		if err != nil {
			log.Error(err)
			return err
		}

		// write header informaiton
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			log.Error(err)
			return err
		}

		var writer io.Writer
		if info.IsDir() {
			baseDir := filepath.Clean(path.Join(fpath, fname))
			log.Debugf("walking base dir: %s", baseDir)
			filepath.Walk(baseDir, func(curpath string, info os.FileInfo, err error) error {
				curpath = filepath.Clean(curpath)

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
				header.Name = filepath.ToSlash(filepath.Clean(header.Name))
				log.Debug(header.Name)

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
				return err
			}
			_, err = io.Copy(writer, ftarget)
			if err != nil {
				log.Error(err)
				return err
			}
		}
	}
	return nil
}
