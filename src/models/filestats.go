package models

import "time"

// FileStats are the file stats transfered to the other
type FileStats struct {
	Name         string
	Size         int64
	ModTime      time.Time
	IsDir        bool
	SentName     string
	IsCompressed bool
	IsEncrypted  bool
}
