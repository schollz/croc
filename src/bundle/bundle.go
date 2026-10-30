// Package bundle defines the small-file-bundles-v1 contract. It knows only
// logical manifest indices; destination paths never occur in bundle messages.
package bundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	Feature               = "small-file-bundles-v1"
	MaxFileBytes    int64 = 1 << 20
	NativeMaxBytes  int64 = 32 << 20
	BrowserMaxBytes int64 = 8 << 20
	MinFiles              = 4
	MaxFiles              = 128
	MaxControlBytes       = 16 << 10
	ChunkSize       int64 = 32 << 10
)

type Limits struct {
	MaxBytes int64 `json:"maxBytes"`
	MaxFiles int   `json:"maxFiles"`
}

func NativeLimits() Limits { return Limits{NativeMaxBytes, MaxFiles} }

func (l Limits) Validate() error {
	if l.MaxBytes <= 0 || l.MaxBytes > NativeMaxBytes || l.MaxFiles < MinFiles || l.MaxFiles > MaxFiles {
		return errors.New("invalid bundle limits")
	}
	return nil
}

func Effective(a, b Limits) (Limits, error) {
	if err := a.Validate(); err != nil {
		return Limits{}, err
	}
	if err := b.Validate(); err != nil {
		return Limits{}, err
	}
	return Limits{min(a.MaxBytes, b.MaxBytes), min(a.MaxFiles, b.MaxFiles)}, nil
}

type Member struct {
	Size      int64
	Eligible  bool
	Requested bool
}

// Layout describes one validated data unit without changing the logical manifest.
type Layout struct {
	Indices []int
	Offsets []int64
	Size    int64
}

// Next resolves only the next candidate. A resolver may perform destination
// preflight; later units are neither inspected nor prepared eagerly.
func Next(count, start int, limits Limits, resolve func(int) (Member, error)) (Layout, error) {
	if err := limits.Validate(); err != nil {
		return Layout{}, err
	}
	var indices []int
	var sizes []int64
	var size int64
	finish := func() Layout {
		if len(indices) < MinFiles && len(indices) > 0 {
			indices, sizes = indices[:1], sizes[:1]
		}
		layout := Layout{Indices: indices, Offsets: make([]int64, len(indices))}
		for i, n := range sizes {
			layout.Offsets[i] = layout.Size
			layout.Size += n
		}
		return layout
	}
	for index := start; index < count; index++ {
		member, err := resolve(index)
		if err != nil {
			return Layout{}, err
		}
		eligible := member.Eligible && member.Size > 0 && member.Size <= MaxFileBytes && member.Size <= limits.MaxBytes
		if !eligible && len(indices) > 0 {
			return finish(), nil
		}
		if !member.Requested {
			continue
		}
		if !eligible {
			return Layout{Indices: []int{index}, Offsets: []int64{0}, Size: member.Size}, nil
		}
		if size+member.Size > limits.MaxBytes {
			return finish(), nil
		}
		indices = append(indices, index)
		sizes = append(sizes, member.Size)
		size += member.Size
		if len(indices) == limits.MaxFiles || size == limits.MaxBytes {
			return finish(), nil
		}
	}
	return finish(), nil
}

// Plan uses the same incremental rules as on-demand native planning.
func Plan(members []Member, limits Limits) ([][]int, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	var units [][]int
	for start := 0; start < len(members); {
		unit, err := Next(len(members), start, limits, func(i int) (Member, error) { return members[i], nil })
		if err != nil {
			return nil, err
		}
		if len(unit.Indices) == 0 {
			break
		}
		units = append(units, unit.Indices)
		start = unit.Indices[len(unit.Indices)-1] + 1
	}
	return units, nil
}

type Request struct {
	ID               uint32 `json:"id"`
	Indices          []int  `json:"indices"`
	Limits           Limits `json:"limits"`
	MachineID        string `json:"MachineID"`
	ExternalIP       string `json:"ExternalIP,omitempty"`
	ReconnectVersion int    `json:"ReconnectVersion"`
}

type Offer struct {
	ID         uint32 `json:"id"`
	Size       int64  `json:"size"`
	Hash       []byte `json:"hash"`
	Compressed bool   `json:"compressed"`
}

type Ack struct {
	ID uint32 `json:"id"`
}

// Decode bounds the inner JSON before parsing any of its contents.
func Decode(payload []byte, target any) error {
	if len(payload) > MaxControlBytes {
		return errors.New("bundle control exceeds 16 KiB")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return fmt.Errorf("invalid bundle control: %w", err)
	}
	var required []string
	switch target.(type) {
	case *Request:
		required = []string{"id", "indices", "limits", "MachineID", "ReconnectVersion"}
	case *Offer:
		required = []string{"id", "size", "hash", "compressed"}
	case *Ack:
		required = []string{"id"}
	}
	for _, name := range required {
		value, ok := fields[name]
		if !ok || bytes.Equal(value, []byte("null")) {
			return fmt.Errorf("missing bundle control field %s", name)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid bundle control: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing bundle control JSON")
	}
	return nil
}

func ValidateRequest(request Request, members []Member, sender Limits) ([]int64, int64, error) {
	limits, err := Effective(sender, request.Limits)
	if err != nil {
		return nil, 0, err
	}
	if request.ID == 0 || len(request.Indices) < MinFiles || len(request.Indices) > limits.MaxFiles {
		return nil, 0, errors.New("invalid bundle request ID or member count")
	}
	offsets := make([]int64, len(request.Indices))
	var size int64
	last := -1
	for i, index := range request.Indices {
		if index <= last || index < 0 || index >= len(members) {
			return nil, 0, errors.New("invalid, duplicate, or unordered bundle index")
		}
		member := members[index]
		if !member.Eligible || member.Size <= 0 || member.Size > MaxFileBytes || member.Size > limits.MaxBytes-size {
			return nil, 0, errors.New("ineligible or oversized bundle member")
		}
		for j := last + 1; last >= 0 && j < index; j++ {
			if !members[j].Eligible {
				return nil, 0, errors.New("bundle crosses ineligible manifest member")
			}
		}
		offsets[i] = size
		size += member.Size
		last = index
	}
	return offsets, size, nil
}

func ValidateOffer(offer Offer, id uint32, size int64, noCompress bool) error {
	if offer.ID != id || id == 0 || offer.Size != size || len(offer.Hash) != 8 || (noCompress && offer.Compressed) {
		return errors.New("bundle offer does not match request")
	}
	return nil
}

// Lifecycle is owned by the attempt's control loop, never by data workers.
type Phase string

const (
	PhaseRequest Phase = "request"
	PhaseOffer   Phase = "offer"
	PhaseReady   Phase = "ready"
	PhaseDone    Phase = "done"
	PhaseClosed  Phase = "closed"
)

type Lifecycle struct {
	LastID   uint32
	ActiveID uint32
	Phase    Phase
}

func (s *Lifecycle) Request(id uint32) error {
	if s.ActiveID != 0 || id == 0 || id <= s.LastID {
		return errors.New("invalid or reused bundle request ID")
	}
	s.LastID, s.ActiveID, s.Phase = id, id, "request"
	return nil
}

func (s *Lifecycle) Advance(id uint32, operation string) error {
	next := map[Phase]Phase{PhaseRequest: PhaseOffer, PhaseOffer: PhaseReady, PhaseReady: PhaseDone, PhaseDone: PhaseClosed}
	if id == 0 || id != s.ActiveID || next[s.Phase] != Phase(operation) {
		return errors.New("invalid bundle lifecycle transition")
	}
	s.Phase = Phase(operation)
	if operation == "closed" {
		s.ActiveID, s.Phase = 0, ""
	}
	return nil
}

// Chunks tracks exact unique chunks. A byte counter alone permits duplicate or
// short middle chunks to masquerade as a complete payload.
type Chunks struct {
	Size  int64
	Seen  []bool
	Bytes int64
}

func NewChunks(size int64) (*Chunks, error) {
	if size <= 0 || size > NativeMaxBytes {
		return nil, errors.New("invalid bundle payload size")
	}
	return &Chunks{Size: size, Seen: make([]bool, (size+ChunkSize-1)/ChunkSize)}, nil
}

// Accept must be serialized by the receiving unit; validate before disk I/O.
func (c *Chunks) Accept(position uint64, length int) (bool, error) {
	if position >= uint64(c.Size) || position%uint64(ChunkSize) != 0 {
		return false, errors.New("unaligned or out-of-range bundle chunk")
	}
	index := position / uint64(ChunkSize)
	if int64(length) != min(ChunkSize, c.Size-int64(position)) || c.Seen[index] {
		return false, errors.New("duplicate or incorrectly sized bundle chunk")
	}
	c.Seen[index] = true
	c.Bytes += int64(length)
	return c.Bytes == c.Size, nil
}
