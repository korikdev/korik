package peer

import (
	"encoding/json"
	"time"
)

// MsgType defines the type of message sent between peers
type MsgType string

const (
	MsgHello        MsgType = "hello"         // initial handshake: send JID, name, pubkey (plaintext, for E2E setup)
	MsgChat         MsgType = "chat"          // chat message, Body is already encrypted (base64 nonce+ciphertext)
	MsgPing         MsgType = "ping"          // keepalive
	MsgHeartbeat    MsgType = "heartbeat"     // periodic liveness probe with timestamp
	MsgTyping       MsgType = "typing"        // typing indicator
	MsgReadReceipt  MsgType = "read_receipt"  // message read confirmation
	MsgRekey        MsgType = "rekey"         // periodic session rekey: PubKey carries fresh ephemeral key
	MsgFileOffer    MsgType = "file_offer"    // file transfer offer
	MsgFileAccept   MsgType = "file_accept"   // file transfer accepted
	MsgFileReject   MsgType = "file_reject"   // file transfer rejected
	MsgFileChunk    MsgType = "file_chunk"    // file data chunk
	MsgFileComplete MsgType = "file_complete" // file transfer completed
	MsgChunkRequest MsgType = "chunk_request" // receiver asks for missing chunk indexes (resume)
	MsgGroupInvite  MsgType = "group_invite"  // room key distribution over pairwise E2E session
	MsgGroupChat    MsgType = "group_chat"    // room message, Body encrypted with the room group key
	MsgGroupLeave   MsgType = "group_leave"   // member departure notice (no auto-rekey in v1)
	MsgSyncRequest  MsgType = "sync_request"  // delta sync: ask for messages newer than SyncSince
	MsgSyncData     MsgType = "sync_data"     // delta sync: re-sent missed messages (fresh envelope IDs)
)

// Message structure sent over TCP connection (transport layer).
// Sensitive fields (chat content) ONLY exist in Body for MsgChat, and it's
// already end-to-end encrypted before being wrapped as Message. Any
// server/relay/intermediary will never see the original chat, only ciphertext.
type Message struct {
	Type      MsgType   `json:"type"`
	ID        string    `json:"id,omitempty"`  // unique envelope ID (sender JID + device + seq + rand)
	Seq       uint64    `json:"seq,omitempty"` // per-sender monotonic sequence
	From      string    `json:"from"`          // display name
	JID       string    `json:"jid"`           // permanent identity of sender (SenderJID)
	LID       string    `json:"lid"`           // short ID of sender
	DeviceID  string    `json:"device_id,omitempty"`
	PubKey    string    `json:"pubkey,omitempty"` // hex pubkey X25519, only filled for MsgHello
	Body      string    `json:"body"`             // plaintext for hello, base64 ciphertext for chat
	Timestamp time.Time `json:"timestamp"`

	// File transfer fields
	FileName   string `json:"file_name,omitempty"`   // original filename
	FileSize   int64  `json:"file_size,omitempty"`   // total file size in bytes
	FileID     string `json:"file_id,omitempty"`     // unique file transfer ID
	ChunkIndex int    `json:"chunk_index,omitempty"` // chunk sequence number
	ChunkTotal int    `json:"chunk_total,omitempty"` // total number of chunks
	ChunkData  string `json:"chunk_data,omitempty"`  // base64 encoded chunk data

	// Read receipt fields
	MessageID string `json:"message_id,omitempty"` // ID of message being acknowledged

	// Rekey / resume fields
	RekeyPubKey string `json:"rekey_pubkey,omitempty"` // ephemeral X25519 pubkey hex for forward secrecy
	Missing     []int  `json:"missing,omitempty"`      // missing chunk indexes requested by receiver

	// Rich messaging fields
	GroupID    string `json:"group_id,omitempty"`   // room ID for group messages
	GroupName  string `json:"group_name,omitempty"` // room name (invite only)
	GroupKey   string `json:"group_key,omitempty"`  // base64 room key (invite only, inside E2E Body channel)
	ReplyTo    string `json:"reply_to,omitempty"`   // quoted envelope ID (short or full)
	Compressed bool   `json:"compressed,omitempty"` // Body plaintext was gzip-compressed before encryption
	SyncSince  int64  `json:"sync_since,omitempty"` // unix seconds for delta sync requests
}

func encodeMessage(m Message) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	b = append(b, '\n') // newline delimiter for framing in TCP stream
	return b, nil
}

func decodeMessage(b []byte) (Message, error) {
	var m Message
	err := json.Unmarshal(b, &m)
	return m, err
}
