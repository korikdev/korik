# korik

[![Go Version](https://img.shields.io/github/go-mod/go-version/korikdev/korik)](go.mod)
[![License](https://img.shields.io/github/license/korikdev/korik)](LICENSE)
[![Release](https://img.shields.io/github/v/release/korikdev/korik)](https://github.com/korikdev/korik/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/korikdev/korik)](https://goreportcard.com/report/github.com/korikdev/korik)

🔐 Secure P2P chat with end-to-end encryption, file transfer, and NAT traversal. Built with Go.

Simple P2P chat CLI featuring **end-to-end encryption**. Supports **LAN/WiFi hotspot** (auto-discovery via UDP broadcast) and **Internet mode** (STUN + UDP hole punching for P2P across networks without relay server).

## Features

- **🔐 End-to-end encryption**: ECDH handshake (X25519) for key exchange, AES-256-GCM encrypted messaging. Pure Go stdlib (`crypto/ecdh`), **zero external dependencies**.
- **🆔 JID & LID**: 
  - **JID** (permanent identity): X25519 keypair stored in `~/.korik/identity.json`. JID = public key fingerprint, consistent across sessions.
  - **LID** (short ID): First 8 characters of JID for compact display.
- **🌐 Internet Mode (NAT Traversal)**:
  - **STUN client** implementation (RFC 5389) from scratch, no external libraries
  - **UDP hole punching** for direct P2P across networks
  - STUN server only for public address discovery, **no traffic relay** (unlike TURN)
  - Trade-off: Fails with symmetric NAT on both sides (inherent to non-relay approach)
- **📡 LAN/WiFi Discovery**:
  - Auto-discovery via UDP broadcast (port 9998)
  - **Fast discovery**: 5x burst @ 200ms, then 1s interval → peers detected < 1 second
  - TCP connections (port 9999) for encrypted messaging
- **💬 Message History**: Persistent local storage of chat history (JSON format)
- **📁 File Transfer**: Send/receive files with chunking, progress tracking, auto-save to `~/Downloads/korik/`
- **📇 Contact Management**: Save, search, and organize contacts
  - Auto-save peer info on first contact
  - Custom aliases/display names
  - Favorites and blocking
  - Search by name, JID, or tags
  - Message count tracking
  - Last seen timestamps
- **⌨️ Typing Indicator**: Real-time typing status broadcast with 3-second timeout
- **✓✓ Read Receipts**: Optional message delivery/read confirmation
- **🔄 Auto-Reconnect**: Automatic reconnection with exponential backoff on disconnect
- **⚙️ Config File**: Persistent settings via `~/.korik/config.json`
- **⚡ Lightweight**: Pure Go, compiled binary ~5-10MB, minimal resource usage

## Installation

### Download Binary (Recommended)

Download the latest binary from [Releases](https://github.com/korikdev/korik/releases):

```bash
# Linux/macOS
wget https://github.com/korikdev/korik/releases/latest/download/korik-$(uname -s)-$(uname -m).tar.gz
tar -xzf korik-*.tar.gz
chmod +x korik
./korik -name your-name

# Or use go install
go install github.com/korikdev/korik@latest
```

### Build from Source

Requires Go 1.21+.

```bash
# Clone repository
git clone https://github.com/korikdev/korik.git
cd korik

# Build
go build -o korik .

# Run
./korik -name your-name
```

## Usage

### LAN/WiFi Mode (Default)

For chatting on the same local network:

```bash
go run . -name alice
```

Open another terminal (another device, but connected to the same WiFi/hotspot):

```bash
go run . -name bob
```

If devices are on the same network (same WiFi/hotspot), they will **automatically discover each other** via UDP broadcast (< 1 second), then establish encrypted TCP connection automatically.

### Internet Mode (P2P Across Networks)

```bash
# Peer 1
./korik -name alice -internet
# Output: "your public address: 203.0.113.5:41001"
# Share candidates to peer 2

# Peer 2  
./korik -name bob -internet -connect-public 203.0.113.5:41001
```

**Note**: 
- STUN server only for address discovery, **not a relay**
- Fails if both sides are behind symmetric NAT (UDP hole punching limitation)
- Default STUN server: `stun.l.google.com:19302`

### Manual Connection (LAN)

If auto-discovery doesn't work (e.g., different subnets or hotspot with AP isolation):

```bash
./korik -name alice -no-discovery
# In CLI prompt:
/connect 192.168.1.5:9999
```

### File Transfer

```bash
# In CLI, send file to connected peer:
/send ~/Documents/presentation.pdf

# On receiving side:
# [📁] alice wants to send file: presentation.pdf (2.5 MB)
# Accept? Type: /accept <file_id>  or  /reject <file_id>

/accept abc123def456

# File automatically saved to ~/Downloads/korik/
```

### Message History

```bash
# View last 20 messages (default)
/history

# View last 50 messages
/history 50

# History persisted in ~/.korik/history.json
```

### Contact Management

```bash
# List all contacts
/contacts

# List favorites only
/contacts favorites

# Search contacts
/search alice

# Show contact details
/contact korik:abc123def456

# Set custom name
/alias korik:abc123def456 Alice Smith

# Mark as favorite
/favorite korik:abc123def456

# Block contact (ignore messages)
/block korik:spam123

# View contact details with stats
/contact korik:abc123def456
# Shows: messages sent, first met, last seen, address, etc.

# Contacts persisted in ~/.korik/contacts.json
```

### Configuration File

Create `~/.korik/config.json`:

```json
{
  "name": "alice",
  "port": 9999,
  "history_size": 5000,
  "typing_indicator": true,
  "auto_reconnect": true
}
```

Then simply run:
```bash
./korik
# All settings loaded from config
```

## Command Line Flags

| Flag | Default | Description |
|---|---|---|
| `-name` | korik-user | Username in chat |
| `-port` | 9999 | TCP port for accepting connections |
| `-dport` | 9998 | UDP port for auto-discovery |
| `-no-discovery` | false | Disable LAN auto-discovery |
| `-connect` | - | Auto-connect to `ip:port` on startup |
| `-internet` | false | Enable internet mode (STUN + hole punching) |
| `-stun-server` | stun.l.google.com:19302 | Public STUN server address |
| `-connect-public` | - | Connect to peer via public address (format: `ip:port`) |
| `-history-size` | 1000 | Maximum messages to keep in history (0 = unlimited) |
| `-read-receipts` | false | Enable read receipts |
| `-typing-indicator` | true | Enable typing indicator |
| `-auto-reconnect` | true | Enable automatic reconnection |

## CLI Commands

Type in CLI prompt:

**Peers & Connection:**
- `/peers` - List connected peers
- `/connect <ip:port>` - Manually connect to peer

**Contacts Management:**
- `/contacts [filter]` - List contacts (all/favorites/recent/blocked)
- `/search <query>` - Search contacts by name/JID
- `/contact <jid>` - Show contact details
- `/alias <jid> <name>` - Set custom name for contact
- `/favorite <jid>` - Mark contact as favorite
- `/block <jid>` - Block contact (ignore messages)
- `/unblock <jid>` - Unblock contact

**Messaging:**
- `/history [n]` - Show last n messages (default: 20)

**File Transfer:**
- `/send <filepath>` - Send file to connected peer
- `/accept <file_id>` - Accept incoming file transfer
- `/reject <file_id>` - Reject incoming file transfer

**Application:**
- `/quit` - Exit application

## Configuration File

Korik supports persistent configuration via `~/.korik/config.json`:

```json
{
  "name": "your-name",
  "port": 9999,
  "discovery_port": 9998,
  "history_size": 1000,
  "stun_server": "stun.l.google.com:19302",
  "no_discovery": false,
  "internet_mode": false,
  "read_receipts": false,
  "typing_indicator": true,
  "auto_reconnect": true
}
```

Command-line flags override config file settings.

## Architecture

### Transport Layers

1. **LAN/WiFi Hotspot**:
   - UDP broadcast (port 9998) → auto-discovery
   - TCP connections (port 9999) → encrypted messaging
   - Fast discovery: burst + 1s interval

2. **Internet (NAT Traversal)**:
   - STUN query → discover public IP:port
   - UDP hole punching → establish P2P connection
   - Shared UDP socket → multiple peer connections via adapter

### Security

- **Key Exchange**: ECDH (X25519) - Elliptic Curve Diffie-Hellman
- **Encryption**: AES-256-GCM (Authenticated Encryption)
- **Identity**: Persistent X25519 keypair → JID (fingerprint)
- **Zero external deps**: Pure `crypto/ecdh` from Go stdlib

## Project Structure

```
korik/
├── .github/
│   ├── workflows/          # CI/CD (GitHub Actions)
│   └── ISSUE_TEMPLATE/     # Bug report & feature request templates
├── internal/
│   ├── identity/
│   │   └── identity.go     # X25519 keypair, JID/LID, persistent storage
│   ├── crypto/
│   │   └── crypto.go       # ECDH key exchange + AES-256-GCM encryption
│   ├── cryptobox/
│   │   └── cryptobox.go    # Additional crypto utilities
│   ├── peer/
│   │   ├── node.go         # TCP connections, E2E handshake, message handling
│   │   ├── message.go      # Message format (JSON)
│   │   ├── discovery.go    # LAN auto-discovery (UDP broadcast, fast burst)
│   │   ├── internet.go     # Internet mode (STUN + UDP hole punching)
│   │   └── udp_adapter.go  # net.Conn adapter for UDP (multi-peer shared socket)
│   ├── nat/
│   │   └── stun.go         # STUN client RFC 5389 (zero deps, pure Go)
│   ├── history/
│   │   └── history.go      # Message history persistence
│   ├── contacts/
│   │   └── contacts.go     # Contact book management
│   ├── filetransfer/
│   │   └── filetransfer.go # File transfer with chunking
│   └── config/
│       └── config.go       # Configuration management
├── main.go                  # Entry point
├── go.mod                   # Go module definition
├── README.md
├── CONTRIBUTING.md          # Contribution guidelines
├── CHANGELOG.md             # Version history
├── .goreleaser.yml          # GoReleaser config for automated releases
├── .golangci.yml            # Linter configuration
├── .korik.example.json      # Example configuration file
└── commitlint.config.js     # Conventional commits validation
```

## Roadmap & Limitations

### Current Limitations

- **Symmetric NAT**: UDP hole punching fails when both peers are behind symmetric NAT (inherent limitation without TURN relay)
- **Group Chat**: Currently broadcast only (pseudo-group "All"), not true multi-peer group
- **File Transfer**: Single peer at a time (multicast file transfer not yet implemented)

### Planned Features

- [ ] Web GUI with SSE/WebSocket
- [ ] True multi-peer group chat
- [ ] Multicast file transfer
- [ ] TURN relay fallback (optional, self-hosted)
- [ ] Voice/video call (WebRTC integration)
- [ ] Mobile apps (React Native or Flutter wrapper)
- [ ] End-to-end file encryption
- [ ] Message editing and deletion

## Contributing

Contributions are very welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Development

```bash
# Setup
go mod download

# Run tests
go test ./...

# Run linter
golangci-lint run

# Build
go build -o korik .
```

### Commit Convention

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(scope): add STUN server support
fix(crypto): prevent panic on invalid key
docs(readme): update installation guide
```

Valid types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`

Valid scopes: `core`, `peer`, `crypto`, `nat`, `cli`, `discovery`, `transport`

## License

[MIT License](LICENSE)

## Acknowledgments

- STUN implementation inspired by RFC 5389
- Crypto primitives from Go standard library
