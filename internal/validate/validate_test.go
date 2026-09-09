package validate

import (
	"testing"
)

func TestPublicKey_Valid(t *testing.T) {
	key := make([]byte, 32)
	if err := PublicKey(key); err != nil {
		t.Fatalf("expected no error for 32-byte key, got %v", err)
	}
}

func TestPublicKey_TooShort(t *testing.T) {
	key := make([]byte, 16)
	if err := PublicKey(key); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestPublicKey_TooLong(t *testing.T) {
	key := make([]byte, 64)
	if err := PublicKey(key); err == nil {
		t.Fatal("expected error for long key")
	}
}

func TestJID_Valid(t *testing.T) {
	jid := "korik:" + "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	if err := JID(jid); err != nil {
		t.Fatalf("expected valid JID, got error: %v", err)
	}
}

func TestJID_NoPrefix(t *testing.T) {
	jid := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	if err := JID(jid); err == nil {
		t.Fatal("expected error for JID without korik: prefix")
	}
}

func TestJID_ShortHex(t *testing.T) {
	jid := "korik:deadbeef"
	if err := JID(jid); err == nil {
		t.Fatal("expected error for JID with short hex")
	}
}

func TestJID_NonHex(t *testing.T) {
	jid := "korik:" + "gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"
	if err := JID(jid); err == nil {
		t.Fatal("expected error for JID with non-hex characters")
	}
}

func TestMessageJSON_Valid(t *testing.T) {
	if err := MessageJSON([]byte(`{"type":"chat","body":"hello"}`)); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestMessageJSON_Empty(t *testing.T) {
	if err := MessageJSON([]byte{}); err == nil {
		t.Fatal("expected error for empty message")
	}
}

func TestMessageJSON_TooLarge(t *testing.T) {
	data := make([]byte, MaxMessageSize+1)
	data[0] = '{'
	if err := MessageJSON(data); err == nil {
		t.Fatal("expected error for oversized message")
	}
}

func TestMessageJSON_NotObject(t *testing.T) {
	if err := MessageJSON([]byte(`["array"]`)); err == nil {
		t.Fatal("expected error for non-object JSON")
	}
}

func TestFilename_Valid(t *testing.T) {
	if err := Filename("document.pdf"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestFilename_Traversal(t *testing.T) {
	if err := Filename("../../etc/passwd"); err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestFilename_NullByte(t *testing.T) {
	if err := Filename("file\x00name"); err == nil {
		t.Fatal("expected error for null byte in filename")
	}
}

func TestFilename_AbsolutePath(t *testing.T) {
	if err := Filename("/etc/passwd"); err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestFilename_Empty(t *testing.T) {
	if err := Filename(""); err == nil {
		t.Fatal("expected error for empty filename")
	}
}

func TestChunkIndex_Valid(t *testing.T) {
	if err := ChunkIndex(0, 10); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if err := ChunkIndex(9, 10); err != nil {
		t.Fatalf("expected no error for last chunk, got %v", err)
	}
}

func TestChunkIndex_Negative(t *testing.T) {
	if err := ChunkIndex(-1, 10); err == nil {
		t.Fatal("expected error for negative index")
	}
}

func TestChunkIndex_OutOfRange(t *testing.T) {
	if err := ChunkIndex(10, 10); err == nil {
		t.Fatal("expected error for index equal to total")
	}
}

func TestChunkIndex_ZeroTotal(t *testing.T) {
	if err := ChunkIndex(0, 0); err == nil {
		t.Fatal("expected error for zero total")
	}
}

func TestNickname_Valid(t *testing.T) {
	tests := []string{"Alice", "Bob Smith", "用户", "a"}
	for _, name := range tests {
		if err := Nickname(name); err != nil {
			t.Fatalf("expected valid nickname %q, got %v", name, err)
		}
	}
}

func TestNickname_Empty(t *testing.T) {
	if err := Nickname(""); err == nil {
		t.Fatal("expected error for empty nickname")
	}
}

func TestNickname_TooLong(t *testing.T) {
	name := "a"
	for i := 0; i < 33; i++ {
		name += "x"
	}
	if err := Nickname(name); err == nil {
		t.Fatal("expected error for nickname exceeding 32 chars")
	}
}

func TestNickname_ControlChars(t *testing.T) {
	if err := Nickname("Alice\x01"); err == nil {
		t.Fatal("expected error for control char in nickname")
	}
}

func TestFileTransferSize_Valid(t *testing.T) {
	if err := FileTransferSize(1024); err != nil {
		t.Fatalf("expected no error for valid size, got %v", err)
	}
}

func TestFileTransferSize_Exceeds2GB(t *testing.T) {
	if err := FileTransferSize(MaxFileTransferSize + 1); err == nil {
		t.Fatal("expected error for size exceeding 2GB")
	}
}

func TestFileTransferSize_Negative(t *testing.T) {
	if err := FileTransferSize(-1); err == nil {
		t.Fatal("expected error for negative size")
	}
}

func TestMessageField_Valid(t *testing.T) {
	if err := MessageField("body", "hello world"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestMessageField_TooLarge(t *testing.T) {
	large := make([]byte, MaxMessageSize+1)
	if err := MessageField("body", string(large)); err == nil {
		t.Fatal("expected error for oversized field")
	}
}
