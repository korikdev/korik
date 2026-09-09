# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Contact Management System**: Comprehensive contact book with persistent storage
  - Auto-save peer info on first contact to `~/.korik/contacts.json`
  - Custom aliases/display names via `/alias` command
  - Favorites system via `/favorite` command
  - Block/unblock contacts (ignored messages from blocked peers)
  - Search contacts by name, JID, notes, or tags
  - Contact filters: all, favorites, recent (7 days), blocked
  - Message count tracking per contact
  - Last seen timestamps with smart formatting (e.g., "2 hours ago")
  - Last known address tracking
  - Detailed contact view with `/contact <jid>` command
  - Commands: `/contacts`, `/search`, `/alias`, `/favorite`, `/block`, `/unblock`, `/contact`

- **Message History & Persistence**: Chat messages now persist locally in `~/.korik/history.json`
  - Configurable history size via `--history-size` flag (default: 1000 messages)
  - `/history [n]` command to view last n messages
  - Automatic history save for sent and received messages
  - Direction indicator (← received, → sent) in history view

- **File Transfer & Multicast**: Send and receive files with progress tracking and multi-peer broadcast
  - `/send <filepath>` command to multicast file transfer offers to all connected peers
  - `/send <filepath> <target>` command to send file transfer offers to a specific peer or group room
  - `/accept <file_id>` and `/reject <file_id>` to respond to offers
  - 64KB chunking for efficient transfer
  - Progress indicator (10% increments)
  - Auto-save to `~/Downloads/korik/`
  - Automatic filename conflict resolution
  - Support for files of any size

- **Typing Indicator**: Real-time typing status broadcast
  - Enabled by default, disable with `--typing-indicator=false`
  - 3-second timeout for stale indicators
  - "[user is typing...]" display in CLI
  - Automatic cleanup of stale typing state

- **Read Receipts**: Message delivery/read confirmation
  - Opt-in with `--read-receipts` flag
  - "✓✓ message read by user" notification
  - Per-peer receipt tracking

- **Auto-Reconnect**: Automatic reconnection on disconnect
  - Enabled by default, disable with `--auto-reconnect=false`
  - Exponential backoff (1s, 2s, 4s, 8s, 16s, max 30s)
  - Max 5 reconnection attempts
  - Status notifications in CLI

- **Config File Support**: Persistent configuration via `~/.korik/config.json`
  - All flags can be set in config file
  - Command-line flags override config values
  - JSON format with default values
  - Example config file: `.korik.example.json`
  - Config fields: name, port, discovery_port, history_size, stun_server, no_discovery, internet_mode, read_receipts, typing_indicator, auto_reconnect

### Changed
- Enhanced CLI command set with contact management features
- Improved peer connection management with reconnection support
- Extended message protocol to support new message types
- Updated README with comprehensive feature documentation
- Broadcast now respects blocked contacts (messages not sent to blocked peers)
- Incoming messages from blocked contacts are silently ignored

### Technical Details
- New modules: `internal/history`, `internal/filetransfer`, `internal/config`, `internal/contacts`
- Extended `Message` type with file transfer and receipt fields
- New message types: `MsgTyping`, `MsgReadReceipt`, `MsgFileOffer`, `MsgFileAccept`, `MsgFileReject`, `MsgFileChunk`, `MsgFileComplete`
- Contact book with auto-save, search, filtering, and blocking capabilities

## [0.1.0] - Initial Release

### Added
- End-to-end encryption with ECDH (X25519) and AES-256-GCM
- LAN/WiFi auto-discovery via UDP broadcast
- Internet mode with STUN client and UDP hole punching
- JID/LID identity system
- TCP-based encrypted messaging
- Basic CLI interface
- Pure Go implementation, zero external dependencies
