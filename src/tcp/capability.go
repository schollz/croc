package tcp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	fastAdmissionPrefix = "croc-fast-admission-v1:"
	fastAdmissionTTL    = 30 * time.Second
)

type fastAdmissionPayload struct {
	Instance string `json:"i"`
	Source   string `json:"s"`
	Parent   string `json:"p"`
	Expires  int64  `json:"e"`
	Max      int    `json:"m"`
	Nonce    string `json:"n"`
}

type fastAdmissionRequest struct {
	Token string `json:"t"`
	Room  string `json:"r"`
}

// RelayCapabilitySet is shared by every port in one relay process. Its key and
// replay state never cross the relay's trust boundary.
type RelayCapabilitySet struct {
	key        []byte
	instance   string
	maxStreams int
	required   int32
	registered atomic.Int32
	mu         sync.Mutex
	used       map[string]time.Time
	now        func() time.Time
}

// NewRelayCapabilitySet creates a capability group for one control port and
// maxStreams advertised data ports.
func NewRelayCapabilitySet(maxStreams int) (*RelayCapabilitySet, error) {
	if maxStreams <= 0 || maxStreams > 8 {
		return nil, fmt.Errorf("fast admission stream count must be between 1 and 8")
	}
	key := make([]byte, 32)
	instanceBytes := make([]byte, 12)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if _, err := rand.Read(instanceBytes); err != nil {
		return nil, err
	}
	return &RelayCapabilitySet{
		key:        key,
		instance:   base64.RawURLEncoding.EncodeToString(instanceBytes),
		maxStreams: maxStreams,
		required:   int32(maxStreams + 1),
		used:       make(map[string]time.Time),
		now:        time.Now,
	}, nil
}

func (s *RelayCapabilitySet) registerPort() {
	if s != nil {
		s.registered.Add(1)
	}
}

func (s *RelayCapabilitySet) canIssue() bool {
	return s != nil && s.registered.Load() >= s.required
}

func (s *RelayCapabilitySet) issue(source, parent string) (string, error) {
	if !s.canIssue() {
		return "", errors.New("not every advertised relay port shares the capability key")
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := fastAdmissionPayload{
		Instance: s.instance,
		Source:   source,
		Parent:   parent,
		Expires:  s.now().Add(fastAdmissionTTL).Unix(),
		Max:      s.maxStreams,
		Nonce:    base64.RawURLEncoding.EncodeToString(nonce),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(encoded)
	token := append(encoded, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

func (s *RelayCapabilitySet) verifyAndUse(source, room, token string) error {
	if s == nil {
		return errors.New("fast admission is unsupported")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) <= sha256.Size {
		return errors.New("invalid relay capability")
	}
	payloadBytes, signature := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(payloadBytes)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errors.New("invalid relay capability")
	}
	var payload fastAdmissionPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return errors.New("invalid relay capability")
	}
	now := s.now()
	if payload.Instance != s.instance || payload.Source != source || payload.Expires < now.Unix() || payload.Max <= 0 || payload.Max > s.maxStreams {
		return errors.New("expired or mismatched relay capability")
	}
	prefix := payload.Parent + "-"
	if !strings.HasPrefix(room, prefix) {
		return errors.New("relay capability room mismatch")
	}
	suffix, err := strconv.Atoi(strings.TrimPrefix(room, prefix))
	if err != nil || suffix < 0 || suffix >= payload.Max {
		return errors.New("relay capability room suffix is not authorized")
	}
	useKey := payload.Nonce + ":" + strconv.Itoa(suffix)
	s.mu.Lock()
	defer s.mu.Unlock()
	for used, expiry := range s.used {
		if expiry.Before(now) {
			delete(s.used, used)
		}
	}
	if _, replayed := s.used[useKey]; replayed {
		return errors.New("relay capability was replayed")
	}
	s.used[useKey] = time.Unix(payload.Expires, 0)
	return nil
}

func encodeFastAdmissionRequest(token, room string) ([]byte, error) {
	payload, err := json.Marshal(fastAdmissionRequest{Token: token, Room: room})
	if err != nil {
		return nil, err
	}
	return append([]byte(fastAdmissionPrefix), payload...), nil
}

func decodeFastAdmissionRequest(payload []byte) (fastAdmissionRequest, bool) {
	if !strings.HasPrefix(string(payload), fastAdmissionPrefix) {
		return fastAdmissionRequest{}, false
	}
	var request fastAdmissionRequest
	err := json.Unmarshal(payload[len(fastAdmissionPrefix):], &request)
	return request, err == nil && request.Token != "" && request.Room != ""
}
