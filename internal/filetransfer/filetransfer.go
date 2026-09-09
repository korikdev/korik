package filetransfer

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	ChunkSize       = 64 * 1024 // 64KB per chunk
	DefaultSaveDir  = "Downloads/korik"
	TransferTimeout = 5 * time.Minute
	MaxConcurrent   = 3 // max concurrent file transfers

	// TransferMaxAge is the maximum duration an incomplete transfer checkpoint
	// is retained on disk before it is automatically purged (Req 8.9).
	TransferMaxAge = 7 * 24 * time.Hour
)

// Transfer represents an active file transfer
type Transfer struct {
	ID         string
	FileName   string
	FileSize   int64
	ChunkTotal int
	Direction  string // "send" or "receive"
	PeerJID    string
	StartedAt  time.Time
	LastUpdate time.Time
	BytesTotal int64
	BytesSent  int64
	Status     string // "pending", "active", "completed", "failed", "canceled"

	// For receiving
	chunks     map[int][]byte
	chunksRecv int

	// For sending
	file       *os.File
	chunksSent int
	sourcePath string // original path, used for random-access resend after close

	mu sync.RWMutex
}

// directionReceive marks transfers writing chunks to disk.
const directionReceive = "receive"

// Manager manages file transfers
type Manager struct {
	transfers  map[string]*Transfer
	mu         sync.RWMutex
	saveDir    string
	partialDir string
}

// NewManager creates a new file transfer manager
func NewManager() *Manager {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	saveDir := filepath.Join(home, DefaultSaveDir)

	return &Manager{
		transfers:  make(map[string]*Transfer),
		saveDir:    saveDir,
		partialDir: filepath.Join(home, ".korik", "partial"),
	}
}

// SetSaveDir sets the directory where received files are saved
func (m *Manager) SetSaveDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveDir = dir
}

// CreateSendTransfer creates a new outgoing file transfer
func (m *Manager) CreateSendTransfer(filePath, peerJID string) (*Transfer, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	fileSize := stat.Size()
	chunkTotal := int((fileSize + ChunkSize - 1) / ChunkSize)

	// Generate unique ID
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", filePath, peerJID, time.Now().UnixNano())))
	id := hex.EncodeToString(hash[:])[:16]

	t := &Transfer{
		ID:         id,
		FileName:   filepath.Base(filePath),
		FileSize:   fileSize,
		ChunkTotal: chunkTotal,
		Direction:  "send",
		PeerJID:    peerJID,
		StartedAt:  time.Now(),
		LastUpdate: time.Now(),
		BytesTotal: fileSize,
		Status:     "pending",
		file:       file,
		sourcePath: filePath,
	}

	m.mu.Lock()
	m.transfers[id] = t
	m.mu.Unlock()

	return t, nil
}

// CreateReceiveTransfer creates a new incoming file transfer
func (m *Manager) CreateReceiveTransfer(id, fileName, peerJID string, fileSize int64, chunkTotal int) *Transfer {
	t := &Transfer{
		ID:         id,
		FileName:   fileName,
		FileSize:   fileSize,
		ChunkTotal: chunkTotal,
		Direction:  directionReceive,
		PeerJID:    peerJID,
		StartedAt:  time.Now(),
		LastUpdate: time.Now(),
		BytesTotal: fileSize,
		Status:     "active",
		chunks:     make(map[int][]byte),
	}

	m.mu.Lock()
	m.transfers[id] = t
	m.mu.Unlock()

	return t
}

// GetTransfer retrieves a transfer by ID
func (m *Manager) GetTransfer(id string) (*Transfer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.transfers[id]
	return t, ok
}

// ReadChunk reads the next chunk from a send transfer
func (t *Transfer) ReadChunk() (chunk []byte, index int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Direction != "send" {
		return nil, 0, fmt.Errorf("not a send transfer")
	}

	if t.chunksSent >= t.ChunkTotal {
		return nil, t.chunksSent, io.EOF
	}

	chunk = make([]byte, ChunkSize)
	n, readErr := t.file.Read(chunk)
	if readErr != nil && readErr != io.EOF {
		return nil, t.chunksSent, readErr
	}

	index = t.chunksSent
	t.chunksSent++
	t.BytesSent += int64(n)
	t.LastUpdate = time.Now()

	if t.chunksSent >= t.ChunkTotal {
		t.Status = "completed"
		_ = t.file.Close()
	} else {
		t.Status = "active"
	}

	return chunk[:n], index, nil
}

// ReadChunkAt reads a specific chunk by index via random access.
// It opens a fresh handle so resends work even after the sequential
// cursor finished and the original file was closed.
func (t *Transfer) ReadChunkAt(index int) ([]byte, error) {
	t.mu.RLock()
	if t.Direction != "send" {
		t.mu.RUnlock()
		return nil, fmt.Errorf("not a send transfer")
	}
	if index < 0 || index >= t.ChunkTotal {
		t.mu.RUnlock()
		return nil, fmt.Errorf("chunk index %d out of range [0,%d)", index, t.ChunkTotal)
	}
	source := t.sourcePath
	t.mu.RUnlock()

	file, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen source file: %w", err)
	}
	defer file.Close()

	offset := int64(index) * ChunkSize
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, ChunkSize)
	n, err := io.ReadFull(file, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf[:n], nil
}

// WriteChunk writes a received chunk
func (t *Transfer) WriteChunk(index int, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Direction != directionReceive {
		return fmt.Errorf("not a receive transfer")
	}

	if _, exists := t.chunks[index]; exists {
		return nil // already received this chunk
	}

	t.chunks[index] = data
	t.chunksRecv++
	t.BytesSent += int64(len(data))
	t.LastUpdate = time.Now()

	return nil
}

// IsComplete checks if all chunks have been received
func (t *Transfer) IsComplete() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.chunksRecv >= t.ChunkTotal
}

// SaveFile saves the received file to disk
func (m *Manager) SaveFile(t *Transfer) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Direction != directionReceive {
		return "", fmt.Errorf("not a receive transfer")
	}

	if !t.IsComplete() {
		return "", fmt.Errorf("transfer not complete")
	}

	// Create save directory
	if err := os.MkdirAll(m.saveDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create save directory: %w", err)
	}

	// Generate unique filename if file exists
	savePath := filepath.Join(m.saveDir, t.FileName)
	if _, err := os.Stat(savePath); err == nil {
		ext := filepath.Ext(t.FileName)
		base := t.FileName[:len(t.FileName)-len(ext)]
		for i := 1; ; i++ {
			savePath = filepath.Join(m.saveDir, fmt.Sprintf("%s_%d%s", base, i, ext))
			if _, err := os.Stat(savePath); os.IsNotExist(err) {
				break
			}
		}
	}

	// Write file
	file, err := os.Create(savePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Write chunks in order
	for i := 0; i < t.ChunkTotal; i++ {
		chunk, ok := t.chunks[i]
		if !ok {
			return "", fmt.Errorf("missing chunk %d", i)
		}
		if _, err := file.Write(chunk); err != nil {
			return "", fmt.Errorf("failed to write chunk: %w", err)
		}
	}

	t.Status = "completed"
	return savePath, nil
}

// Cancel cancels a transfer
func (t *Transfer) Cancel() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.Status = "canceled"
	if t.file != nil {
		t.file.Close()
	}
}

// Progress returns transfer progress (0.0 to 1.0)
func (t *Transfer) Progress() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.BytesTotal == 0 {
		return 0
	}
	return float64(t.BytesSent) / float64(t.BytesTotal)
}

// ProgressPercent returns transfer progress as percentage
func (t *Transfer) ProgressPercent() int {
	return int(t.Progress() * 100)
}

// EncodeChunk encodes chunk data to base64
func EncodeChunk(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeChunk decodes base64 chunk data
func DecodeChunk(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}

// MissingChunks returns indexes not yet received, enabling resume.
func (t *Transfer) MissingChunks() []int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.Direction != directionReceive {
		return nil
	}
	missing := make([]int, 0)
	for i := 0; i < t.ChunkTotal; i++ {
		if _, ok := t.chunks[i]; !ok {
			missing = append(missing, i)
		}
	}
	return missing
}

// ReceivedCount returns how many chunks have arrived so far.
func (t *Transfer) ReceivedCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.chunksRecv
}

// Checkpoint persists received chunks to disk so an interrupted
// transfer can resume instead of restarting from zero.
func (m *Manager) Checkpoint(t *Transfer) error {
	t.mu.RLock()
	if t.Direction != directionReceive {
		t.mu.RUnlock()
		return nil
	}
	snapshot := make(map[int][]byte, len(t.chunks))
	for k, v := range t.chunks {
		cp := make([]byte, len(v))
		copy(cp, v)
		snapshot[k] = cp
	}
	meta := struct {
		ID         string `json:"id"`
		FileName   string `json:"file_name"`
		FileSize   int64  `json:"file_size"`
		ChunkTotal int    `json:"chunk_total"`
		PeerJID    string `json:"peer_jid"`
	}{t.ID, t.FileName, t.FileSize, t.ChunkTotal, t.PeerJID}
	t.mu.RUnlock()

	if err := os.MkdirAll(m.partialDir, 0o700); err != nil {
		return err
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.partialDir, t.ID+".meta"), metaData, 0o600); err != nil {
		return err
	}
	combined := make(map[string]string, len(snapshot))
	for k, v := range snapshot {
		combined[strconv.Itoa(k)] = EncodeChunk(v)
	}
	chunkData, err := json.Marshal(combined)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.partialDir, t.ID+".chunks"), chunkData, 0o600)
}

// RestorePartial reloads a checkpointed transfer, if present.
func (m *Manager) RestorePartial(id string) (*Transfer, bool) {
	metaPath := filepath.Join(m.partialDir, id+".meta")
	chunkPath := filepath.Join(m.partialDir, id+".chunks")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, false
	}
	var meta struct {
		ID         string `json:"id"`
		FileName   string `json:"file_name"`
		FileSize   int64  `json:"file_size"`
		ChunkTotal int    `json:"chunk_total"`
		PeerJID    string `json:"peer_jid"`
	}
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, false
	}
	chunkData, err := os.ReadFile(chunkPath)
	if err != nil {
		return nil, false
	}
	var combined map[string]string
	if err := json.Unmarshal(chunkData, &combined); err != nil {
		return nil, false
	}
	t := &Transfer{
		ID:         meta.ID,
		FileName:   meta.FileName,
		FileSize:   meta.FileSize,
		ChunkTotal: meta.ChunkTotal,
		Direction:  directionReceive,
		PeerJID:    meta.PeerJID,
		StartedAt:  time.Now(),
		LastUpdate: time.Now(),
		BytesTotal: meta.FileSize,
		Status:     "active",
		chunks:     make(map[int][]byte),
	}
	for k, v := range combined {
		idx, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		raw, err := DecodeChunk(v)
		if err != nil {
			continue
		}
		t.chunks[idx] = raw
		t.chunksRecv++
		t.BytesSent += int64(len(raw))
	}
	m.mu.Lock()
	m.transfers[id] = t
	m.mu.Unlock()
	return t, true
}

// ClearPartial removes checkpoint files after completion or cancel.
func (m *Manager) ClearPartial(id string) {
	_ = os.Remove(filepath.Join(m.partialDir, id+".meta"))
	_ = os.Remove(filepath.Join(m.partialDir, id+".chunks"))
}

// PruneExpiredPartials removes incomplete transfer checkpoints older than
// TransferMaxAge (7 days) from the partial directory. It should be called
// on startup and periodically to prevent stale files from accumulating.
func (m *Manager) PruneExpiredPartials() {
	entries, err := os.ReadDir(m.partialDir)
	if err != nil {
		return // directory may not exist yet; silently skip
	}

	cutoff := time.Now().Add(-TransferMaxAge)

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".meta" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			// Derive transfer ID from the .meta filename and remove both files.
			id := entry.Name()[:len(entry.Name())-len(".meta")]
			m.ClearPartial(id)
		}
	}
}
