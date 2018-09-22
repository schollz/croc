package models

import "time"

type FileStats struct {
	Name    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}
