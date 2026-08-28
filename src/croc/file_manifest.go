package croc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/schollz/croc/v11/src/comm"
	"github.com/schollz/croc/v11/src/message"
)

const (
	chunkedFileInfoFeature        = "chunked-fileinfo-v1"
	maxFileInfoBatchJSONSize      = 8 * 1024 * 1024
	maxChunkedManifestEntries     = 1_000_000
	maxChunkedManifestJSONSize    = 512 * 1024 * 1024
	fileInfoBatchKindFiles        = "f"
	fileInfoBatchKindEmptyFolders = "d"
)

type fileInfoManifestLimits struct {
	maxBatchJSONSize    int
	maxEntries          int
	maxManifestJSONSize int64
}

var defaultFileInfoManifestLimits = fileInfoManifestLimits{
	maxBatchJSONSize:    maxFileInfoBatchJSONSize,
	maxEntries:          maxChunkedManifestEntries,
	maxManifestJSONSize: maxChunkedManifestJSONSize,
}

type fileInfoManifestHeader struct {
	FileCount          int      `json:"fc"`
	EmptyFolderCount   int      `json:"ec"`
	TotalNumberFolders int      `json:"tf"`
	MachineID          string   `json:"mi"`
	ExternalIP         string   `json:"ip,omitempty"`
	Ask                bool     `json:"a"`
	SendingText        bool     `json:"st"`
	NoCompress         bool     `json:"nc"`
	HashAlgorithm      string   `json:"ha"`
	ReconnectVersion   int      `json:"rv"`
	NextReconnectRoom  string   `json:"nr,omitempty"`
	Features           []string `json:"f,omitempty"`
}

type fileInfoManifestBatch struct {
	Kind    string     `json:"k"`
	Entries []FileInfo `json:"e"`
}

type incomingFileManifest struct {
	header       fileInfoManifestHeader
	limits       fileInfoManifestLimits
	nextBatch    int
	decodedBytes int64
	files        []FileInfo
	emptyFolders []FileInfo
}

func fileInfoManifestHeaderFromSenderInfo(info SenderInfo) fileInfoManifestHeader {
	return fileInfoManifestHeader{
		FileCount:          len(info.FilesToTransfer),
		EmptyFolderCount:   len(info.EmptyFoldersToTransfer),
		TotalNumberFolders: info.TotalNumberFolders,
		MachineID:          info.MachineID,
		ExternalIP:         info.ExternalIP,
		Ask:                info.Ask,
		SendingText:        info.SendingText,
		NoCompress:         info.NoCompress,
		HashAlgorithm:      info.HashAlgorithm,
		ReconnectVersion:   info.ReconnectVersion,
		NextReconnectRoom:  info.NextReconnectRoom,
		Features:           info.Features,
	}
}

func (h fileInfoManifestHeader) senderInfo(files, emptyFolders []FileInfo) SenderInfo {
	return SenderInfo{
		FilesToTransfer:        files,
		EmptyFoldersToTransfer: emptyFolders,
		TotalNumberFolders:     h.TotalNumberFolders,
		MachineID:              h.MachineID,
		ExternalIP:             h.ExternalIP,
		Ask:                    h.Ask,
		SendingText:            h.SendingText,
		NoCompress:             h.NoCompress,
		HashAlgorithm:          h.HashAlgorithm,
		ReconnectVersion:       h.ReconnectVersion,
		NextReconnectRoom:      h.NextReconnectRoom,
		Features:               h.Features,
	}
}

func fileInfoForWire(info FileInfo) FileInfo {
	info.FolderSource = ""
	info.IsIgnored = false
	return info
}

func fileInfosForWire(infos []FileInfo) []FileInfo {
	if infos == nil {
		return nil
	}
	wire := make([]FileInfo, len(infos))
	for i, info := range infos {
		wire[i] = fileInfoForWire(info)
	}
	return wire
}

func (c *Client) sendSenderInfo(info SenderInfo) error {
	if c.peerChunkedFileInfo {
		return c.sendChunkedSenderInfo(info)
	}

	return c.sendLegacySenderInfo(info)
}

func (c *Client) sendLegacySenderInfo(info SenderInfo) error {
	info.FilesToTransfer = fileInfosForWire(info.FilesToTransfer)
	info.EmptyFoldersToTransfer = fileInfosForWire(info.EmptyFoldersToTransfer)
	payload, err := json.Marshal(info)
	if err != nil {
		return err
	}
	err = message.Send(c.connection(0), c.Key, message.Message{
		Type:  message.TypeFileInfo,
		Bytes: payload,
	})
	return wrapLegacyFileInfoSendError(err)
}

func wrapLegacyFileInfoSendError(err error) error {
	if errors.Is(err, message.ErrMessageTooLarge) || errors.Is(err, comm.ErrMessageTooLarge) {
		return fmt.Errorf("file list is too large for this recipient; update croc on the receiving computer or zip/split the directory: %w", err)
	}
	return err
}

func (c *Client) sendChunkedSenderInfo(info SenderInfo) error {
	return emitChunkedSenderInfo(info, defaultFileInfoManifestLimits, func(m message.Message) error {
		return message.Send(c.connection(0), c.Key, m)
	})
}

func emitChunkedSenderInfo(info SenderInfo, limits fileInfoManifestLimits, emit func(message.Message) error) error {
	if err := limits.validate(); err != nil {
		return err
	}
	fileCount := len(info.FilesToTransfer)
	emptyFolderCount := len(info.EmptyFoldersToTransfer)
	if fileCount > limits.maxEntries || emptyFolderCount > limits.maxEntries-fileCount {
		return fmt.Errorf(
			"file manifest contains %d files and %d empty folders; maximum combined entry count is %d",
			fileCount, emptyFolderCount, limits.maxEntries,
		)
	}

	header := fileInfoManifestHeaderFromSenderInfo(info)
	headerPayload, err := json.Marshal(header)
	if err != nil {
		return err
	}
	decodedBytes := int64(len(headerPayload))
	if decodedBytes > limits.maxManifestJSONSize {
		return fmt.Errorf("file manifest metadata exceeds maximum size: %d > %d", decodedBytes, limits.maxManifestJSONSize)
	}
	if err = emit(message.Message{
		Type:  message.TypeFileInfoStart,
		Bytes: headerPayload,
	}); err != nil {
		return err
	}

	batchNumber := 0
	emitBatch := func(payload []byte) error {
		decodedBytes += int64(len(payload))
		if decodedBytes > limits.maxManifestJSONSize {
			return fmt.Errorf("file manifest metadata exceeds maximum size: %d > %d", decodedBytes, limits.maxManifestJSONSize)
		}
		if err := emit(message.Message{
			Type:  message.TypeFileInfoBatch,
			Num:   batchNumber,
			Bytes: payload,
		}); err != nil {
			return err
		}
		batchNumber++
		return nil
	}
	if err = encodeFileInfoBatches(info.FilesToTransfer, fileInfoBatchKindFiles, limits.maxBatchJSONSize, emitBatch); err != nil {
		return err
	}
	if err = encodeFileInfoBatches(info.EmptyFoldersToTransfer, fileInfoBatchKindEmptyFolders, limits.maxBatchJSONSize, emitBatch); err != nil {
		return err
	}
	return emit(message.Message{
		Type: message.TypeFileInfoEnd,
		Num:  batchNumber,
	})
}

func (l fileInfoManifestLimits) validate() error {
	if l.maxBatchJSONSize <= 0 {
		return errors.New("file metadata batch limit must be positive")
	}
	if l.maxEntries < 0 {
		return errors.New("file manifest entry limit cannot be negative")
	}
	if l.maxManifestJSONSize <= 0 {
		return errors.New("file manifest metadata limit must be positive")
	}
	return nil
}

func encodeFileInfoBatches(infos []FileInfo, kind string, maxPayloadSize int, emit func([]byte) error) error {
	prefix := []byte(`{"k":"` + kind + `","e":[`)
	suffix := []byte(`]}`)
	if maxPayloadSize < len(prefix)+len(suffix) {
		return fmt.Errorf("file metadata batch limit %d is too small", maxPayloadSize)
	}

	var encodedEntries [][]byte
	batchSize := len(prefix) + len(suffix)
	flush := func() error {
		if len(encodedEntries) == 0 {
			return nil
		}
		var payload bytes.Buffer
		payload.Grow(batchSize)
		payload.Write(prefix)
		for i, encoded := range encodedEntries {
			if i > 0 {
				payload.WriteByte(',')
			}
			payload.Write(encoded)
		}
		payload.Write(suffix)
		if payload.Len() > maxPayloadSize {
			return fmt.Errorf("file metadata batch exceeds maximum size: %d > %d", payload.Len(), maxPayloadSize)
		}
		if err := emit(payload.Bytes()); err != nil {
			return err
		}
		encodedEntries = encodedEntries[:0]
		batchSize = len(prefix) + len(suffix)
		return nil
	}

	for _, info := range infos {
		encoded, err := json.Marshal(fileInfoForWire(info))
		if err != nil {
			return err
		}
		entrySize := len(encoded)
		if len(encodedEntries) > 0 {
			entrySize++
		}
		if batchSize+entrySize > maxPayloadSize {
			if len(encodedEntries) == 0 {
				return fmt.Errorf("single file metadata entry exceeds batch limit: %d > %d", batchSize+entrySize, maxPayloadSize)
			}
			if err := flush(); err != nil {
				return err
			}
			entrySize = len(encoded)
		}
		if batchSize+entrySize > maxPayloadSize {
			return fmt.Errorf("single file metadata entry exceeds batch limit: %d > %d", batchSize+entrySize, maxPayloadSize)
		}
		encodedEntries = append(encodedEntries, encoded)
		batchSize += entrySize
	}
	return flush()
}

func (c *Client) validateChunkedFileInfoMessage() error {
	if c.Options.IsSender {
		return errors.New("sender received chunked file metadata")
	}
	if !c.peerChunkedFileInfo {
		return errors.New("peer sent chunked file metadata without negotiating chunked-fileinfo-v1")
	}
	if len(c.Key) == 0 || !c.lifecycleSnapshot().ChannelSecured {
		return errors.New("peer sent chunked file metadata before the secure channel was established")
	}
	return nil
}

func validateFileInfoManifestHeader(header fileInfoManifestHeader, maxEntries int) error {
	if header.FileCount < 0 || header.EmptyFolderCount < 0 {
		return errors.New("file manifest contains a negative entry count")
	}
	if header.FileCount > maxEntries ||
		header.EmptyFolderCount > maxEntries-header.FileCount {
		return fmt.Errorf("file manifest entry count exceeds maximum of %d", maxEntries)
	}
	return nil
}

func (c *Client) clearIncomingFileManifest() {
	c.incomingFileManifest = nil
}

func (c *Client) rejectIncomingFileManifest(err error) error {
	c.clearIncomingFileManifest()
	return err
}

func (c *Client) processMessageFileInfoStart(m message.Message) error {
	return c.processMessageFileInfoStartWithLimits(m, defaultFileInfoManifestLimits)
}

func (c *Client) processMessageFileInfoStartWithLimits(m message.Message, limits fileInfoManifestLimits) error {
	if err := limits.validate(); err != nil {
		return c.rejectIncomingFileManifest(err)
	}
	if err := c.validateChunkedFileInfoMessage(); err != nil {
		return c.rejectIncomingFileManifest(err)
	}
	if c.incomingFileManifest != nil {
		return c.rejectIncomingFileManifest(errors.New("duplicate file manifest start"))
	}
	var header fileInfoManifestHeader
	if err := json.Unmarshal(m.Bytes, &header); err != nil {
		return c.rejectIncomingFileManifest(fmt.Errorf("decode file manifest header: %w", err))
	}
	if err := validateFileInfoManifestHeader(header, limits.maxEntries); err != nil {
		return c.rejectIncomingFileManifest(err)
	}
	if int64(len(m.Bytes)) > limits.maxManifestJSONSize {
		return c.rejectIncomingFileManifest(fmt.Errorf("file manifest metadata exceeds maximum size: %d > %d", len(m.Bytes), limits.maxManifestJSONSize))
	}
	c.incomingFileManifest = &incomingFileManifest{
		header:       header,
		limits:       limits,
		decodedBytes: int64(len(m.Bytes)),
	}

	// These fields are needed to retry if the connection drops before the end
	// of a large manifest. All other sender settings are applied at completion.
	c.peerReconnectVersion = header.ReconnectVersion
	c.nextReconnectRoom = header.NextReconnectRoom
	return nil
}

func (c *Client) processMessageFileInfoBatch(m message.Message) error {
	if err := c.validateChunkedFileInfoMessage(); err != nil {
		return c.rejectIncomingFileManifest(err)
	}
	manifest := c.incomingFileManifest
	if manifest == nil {
		return errors.New("file manifest batch arrived before start")
	}
	if m.Num != manifest.nextBatch {
		return c.rejectIncomingFileManifest(fmt.Errorf("unexpected file manifest batch %d; wanted %d", m.Num, manifest.nextBatch))
	}
	if len(m.Bytes) > manifest.limits.maxBatchJSONSize {
		return c.rejectIncomingFileManifest(fmt.Errorf("file metadata batch exceeds maximum size: %d > %d", len(m.Bytes), manifest.limits.maxBatchJSONSize))
	}
	if manifest.decodedBytes+int64(len(m.Bytes)) > manifest.limits.maxManifestJSONSize {
		return c.rejectIncomingFileManifest(fmt.Errorf("file manifest metadata exceeds maximum size: %d > %d", manifest.decodedBytes+int64(len(m.Bytes)), manifest.limits.maxManifestJSONSize))
	}

	var batch fileInfoManifestBatch
	if err := json.Unmarshal(m.Bytes, &batch); err != nil {
		return c.rejectIncomingFileManifest(fmt.Errorf("decode file manifest batch: %w", err))
	}
	if len(batch.Entries) == 0 {
		return c.rejectIncomingFileManifest(errors.New("file manifest batch is empty"))
	}
	for i := range batch.Entries {
		batch.Entries[i] = fileInfoForWire(batch.Entries[i])
	}
	switch batch.Kind {
	case fileInfoBatchKindFiles:
		if len(batch.Entries) > manifest.header.FileCount-len(manifest.files) {
			return c.rejectIncomingFileManifest(errors.New("file manifest contains more files than declared"))
		}
		manifest.files = append(manifest.files, batch.Entries...)
	case fileInfoBatchKindEmptyFolders:
		if len(batch.Entries) > manifest.header.EmptyFolderCount-len(manifest.emptyFolders) {
			return c.rejectIncomingFileManifest(errors.New("file manifest contains more empty folders than declared"))
		}
		manifest.emptyFolders = append(manifest.emptyFolders, batch.Entries...)
	default:
		return c.rejectIncomingFileManifest(fmt.Errorf("unknown file manifest batch kind %q", batch.Kind))
	}
	manifest.decodedBytes += int64(len(m.Bytes))
	manifest.nextBatch++
	return nil
}

func (c *Client) processMessageFileInfoEnd(m message.Message) (done bool, err error) {
	if err = c.validateChunkedFileInfoMessage(); err != nil {
		return false, c.rejectIncomingFileManifest(err)
	}
	manifest := c.incomingFileManifest
	if manifest == nil {
		return false, errors.New("file manifest end arrived before start")
	}
	if len(m.Bytes) != 0 {
		return false, c.rejectIncomingFileManifest(errors.New("file manifest end contains unexpected data"))
	}
	if m.Num != manifest.nextBatch {
		return false, c.rejectIncomingFileManifest(fmt.Errorf("unexpected file manifest end %d; wanted %d", m.Num, manifest.nextBatch))
	}
	if len(manifest.files) != manifest.header.FileCount || len(manifest.emptyFolders) != manifest.header.EmptyFolderCount {
		return false, c.rejectIncomingFileManifest(fmt.Errorf(
			"incomplete file manifest: received %d/%d files and %d/%d empty folders",
			len(manifest.files), manifest.header.FileCount,
			len(manifest.emptyFolders), manifest.header.EmptyFolderCount,
		))
	}

	info := manifest.header.senderInfo(manifest.files, manifest.emptyFolders)
	c.clearIncomingFileManifest()
	return c.processSenderInfo(info)
}
