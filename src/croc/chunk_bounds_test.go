package croc

import (
	"math"
	"testing"
)

func TestChunkFitsDeclaredFileSize(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		position uint64
		length   int
		wantErr  bool
	}{
		{name: "exact", size: 8, position: 0, length: 8},
		{name: "partial", size: 8, position: 3, length: 5},
		{name: "payload too large", size: 8, position: 0, length: 9, wantErr: true},
		{name: "past end", size: 8, position: 8, length: 1, wantErr: true},
		{name: "position overflow", size: 8, position: uint64(1) << 63, length: 1, wantErr: true},
		{name: "maximum position overflow", size: math.MaxInt64, position: math.MaxUint64, length: 1, wantErr: true},
		{name: "negative declared size", size: -1, position: 0, length: 1, wantErr: true},
		{name: "negative payload length", size: 8, position: 0, length: -1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := chunkFits(tt.size, tt.position, tt.length)
			if (err != nil) != tt.wantErr {
				t.Fatalf("chunkFits(%d, %d, %d) error = %v, wantErr %v", tt.size, tt.position, tt.length, err, tt.wantErr)
			}
		})
	}
}
