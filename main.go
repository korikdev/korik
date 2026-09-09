package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"korik/internal/audit"
	"korik/internal/autoupdate"
	"korik/internal/backup"
	"korik/internal/config"
	"korik/internal/contacts"
	"korik/internal/debug"
	"korik/internal/group"
	"korik/internal/history"
	"korik/internal/identity"
	"korik/internal/nat"
	"korik/internal/notify"
	"korik/internal/peer"
	"korik/internal/queue"
	"korik/internal/setup"
	"korik/internal/shell"
	"korik/internal/status"
	"korik/internal/ui"
	"korik/internal/verify"

	"golang.org/x/term"
)

var timestampMode = ui.TimestampAbsolute

func main() {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		fmt.Println(ui.Failure("Could not load configuration. Check ~/.korik permissions."))
		os.Exit(1)
	}
	if cfg.EnsureDeviceID() {
		_ = cfg.Save(config.DefaultConfigPath())
	}

	firstRun := setup.IsFirstRun(config.DefaultConfigPath(), identity.DefaultIdentityPath())

	name := flag.String("name", cfg.Name, "your name in chat")
	port := flag.Int("port", cfg.Port, "TCP port for chat")
	discoveryPort := flag.Int("dport", cfg.DiscoveryPort, "UDP port for auto-discovery on LAN/hotspot")
	noDiscovery := flag.Bool("no-discovery", cfg.NoDiscovery, "disable auto-discovery, manual connect only")
	connect := flag.String("connect", "", "connect directly to peer ip:port (optional)")
	internetMode := flag.Bool("internet", cfg.InternetMode, "enable internet mode (STUN + UDP hole punching), for P2P across networks without relay server")
	stunServer := flag.String("stun-server", cfg.STUNServer, "public STUN server address to detect our public address")
	connectPublic := flag.String("connect-public", "", "connect to peer via their STUN-discovered public address (internet mode), format ip:port")
	historySize := flag.Int("history-size", cfg.HistorySize, "maximum number of messages to keep in history (0 = unlimited)")
	readReceipts := flag.Bool("read-receipts", cfg.ReadReceipts, "enable read receipts")
	typingIndicator := flag.Bool("typing-indicator", cfg.TypingIndicator, "enable typing indicator")
	autoReconnect := flag.Bool("auto-reconnect", cfg.AutoReconnect, "enable automatic reconnection on disconnect")
	debugFlag := flag.Bool("debug", false, "enable debug logging")
	noUpdateCheck := flag.Bool("no-update-check", false, "disable automatic update checking on startup")
	flag.Parse()

	if *debugFlag {
		debug.Init(true)
	}

	homeDir, _ := os.UserHomeDir()
	updateChecker := autoupdate.NewChecker("v1.0.0", filepath.Join(homeDir, ".korik"))
	if *noUpdateCheck {
		updateChecker.SetNoCheck(true)
	} else {
		go func() {
			res := <-updateChecker.CheckAsync()
			if res != nil && res.UpdateInfo.HasUpdate {
				fmt.Println(ui.Hint(fmt.Sprintf("\n🔔 New update available: %s (current: %s). Release notes: %s", res.UpdateInfo.Version, res.UpdateInfo.CurrentVersion, res.UpdateInfo.NotesURL)))
			}
		}()
	}

	// Interactive setup wizard on first run when no explicit name was given.
	if firstRun && *name == "korik-user" {
		reader := bufio.NewReader(os.Stdin)
		wizardName, wizardInternet := setup.RunWizard(cfg, reader)
		*name = wizardName
		*internetMode = wizardInternet
		// Persist wizard choices immediately.
		_ = cfg.Save(config.DefaultConfigPath())
	}

	id, err := identity.LoadOrCreate(identity.DefaultIdentityPath())
	if err != nil {
		fmt.Println(ui.Failure(status.HumanizeError(err)))
		os.Exit(1)
	}

	hist, err := history.NewWithTail(history.DefaultHistoryPath(), *historySize, 500)
	if err != nil {
		fmt.Println(ui.Failure(status.HumanizeError(err)))
		os.Exit(1)
	}
	// Expire old disappearing messages on startup.
	_ = hist.PruneExpired()
	if total := hist.TotalStored(); total > 500 {
		fmt.Println(ui.Hint(fmt.Sprintf("history: showing last 500 of %d stored (lazy-loaded)", total)))
	}

	contactBook, err := contacts.NewContactBook(contacts.DefaultContactsPath())
	if err != nil {
		fmt.Println(ui.Failure(status.HumanizeError(err)))
		os.Exit(1)
	}

	msgQueue, err := queue.New(queue.DefaultQueuePath())
	if err != nil {
		fmt.Println(ui.Failure(status.HumanizeError(err)))
		os.Exit(1)
	}

	node := peer.NewNode(*name, *port, *discoveryPort, id)
	node.DeviceID = cfg.DeviceID
	if node.DeviceID == "" {
		node.DeviceID = "cli-1"
	}
	node.History = hist
	node.ContactBook = contactBook
	node.ReadReceipts = *readReceipts
	node.TypingIndicator = *typingIndicator
	node.AutoReconnect = *autoReconnect
	node.MsgQueue = msgQueue
	if groups, err := group.New(group.DefaultGroupsPath()); err != nil {
		fmt.Println(ui.Failure(status.HumanizeError(err)))
		os.Exit(1)
	} else {
		node.Groups = groups
	}
	node.Status.OnChange = func(phase status.Phase, detail string) {
		fmt.Println(ui.Hint("status: " + string(phase) + " " + detail))
	}

	// Graceful shutdown: flush is automatic (queue persists on enqueue),
	// so just close sockets cleanly on /quit, SIGINT, or SIGTERM.
	shutdown := func() {
		fmt.Println(ui.System("shutting down: closing sockets..."))
		node.Stop()
		fmt.Println(ui.System("bye"))
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		fmt.Println()
		shutdown()
		os.Exit(0)
	}()

	if *typingIndicator {
		node.StartTypingCleaner()
	}
	node.StartHeartbeat(15 * time.Second)

	node.OnChat = func(lid, jid, peerName, body string, ts time.Time) {
		fmt.Println()
		fmt.Println(ui.Incoming(peerName, body, ts, timestampMode))
		notify.Send("korik: "+peerName, body)
		fmt.Print("> ")
	}

	node.OnGroupChat = func(groupID, lid, jid, peerName, body, replyTo string, ts time.Time) {
		fmt.Println()
		fmt.Println(ui.Incoming("["+groupID+"] "+peerName, renderReply(hist, replyTo, body), ts, timestampMode))
		notify.Send("korik ["+groupID+"]: "+peerName, body)
		fmt.Print("> ")
	}

	if err := node.Start(); err != nil {
		fmt.Println(ui.Failure(status.HumanizeError(err)))
		os.Exit(1)
	}

	if !*noDiscovery {
		node.Status.Set(status.PhaseDiscovering, "broadcasting on LAN")
		go node.StartDiscovery()
	}

	if *connect != "" {
		go func() {
			if err := node.ConnectWithRetry(*connect, 5, func(line string) {
				fmt.Println(ui.Hint(line))
			}); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			}
		}()
	}

	if *connectPublic != "" {
		go func() {
			if err := node.ConnectPeerPublicAddr(*connectPublic); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			}
		}()
	}

	if *internetMode {
		node.Status.Set(status.PhaseTraversing, "querying STUN "+*stunServer)
		pub, err := node.StartInternet(*stunServer)
		if err != nil {
			fmt.Println(ui.Failure(status.HumanizeError(err)))
		} else {
			addr := pub.String()
			node.Status.Set(status.PhaseConnected, "internet mode, public "+addr)
			fmt.Println(ui.Success("internet mode active. Public address: " + addr))
			fmt.Println(ui.Hint("Share candidates so the peer can try LAN → IPv6 → public → punching."))
			printCandidates(node)
		}
	}

	printBanner(*name, id.JID, id.LID, *port, *discoveryPort, msgQueue.Len())

	commands := []string{
		"/peers", "/devices", "/candidates", "/connect", "/connect-candidates",
		"/retry", "/status", "/history", "/send",
		"/accept", "/reject", "/resume", "/queue", "/contacts", "/search",
		"/contact", "/alias", "/favorite", "/block", "/unblock",
		"/verify", "/safety", "/export", "/import", "/disappearing", "/ephemeral",
		"/group", "/groups", "/g", "/reply", "/test-nat", "/trust",
		"/link-device", "/export-identity", "/import-identity",
		"/export-data", "/import-data", "/check-update",
		"/timestamp", "/help", "/quit",
	}
	home, _ := os.UserHomeDir()
	sh := shell.New(filepath.Join(home, ".korik", "shell_history"), commands)
	sh.SetContactProvider(func() []string {
		names := make([]string, 0)
		for _, c := range contactBook.List("") {
			names = append(names, c.GetDisplayName())
		}
		return names
	})

	var lastInput time.Time
	var typingSent bool

	if *typingIndicator {
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				if time.Since(lastInput) > 0 && time.Since(lastInput) < 2*time.Second && !typingSent {
					node.SendTyping()
					typingSent = true
				} else if time.Since(lastInput) >= 3*time.Second {
					typingSent = false
				}
			}
		}()
	}

	// Periodic expiry of disappearing messages.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			_ = hist.PruneExpired()
		}
	}()

	for {
		line, err := sh.ReadLine("> ")
		if err != nil {
			if err == io.EOF {
				fmt.Println(ui.System("bye"))
				return
			}
			fmt.Println(ui.Failure(status.HumanizeError(err)))
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sh.AddHistory(line)
		typingSent = false

		switch {
		case line == "/quit":
			shutdown()
			return

		case line == "/help":
			printHelp()

		case line == "/peers":
			node.ListPeers()

		case line == "/candidates":
			printCandidates(node)

		case strings.HasPrefix(line, "/connect-candidates "):
			encoded := strings.TrimSpace(strings.TrimPrefix(line, "/connect-candidates "))
			bundle, err := nat.DecodeBundle(encoded)
			if err != nil {
				fmt.Println(ui.Failure("Invalid bundle. Paste the full output of peer /candidates."))
				continue
			}
			go func() {
				err := node.ConnectWithCandidates(bundle, func(step nat.StrategyStep, msg string) {
					fmt.Println(ui.Hint("[" + step.String() + "] " + msg))
				})
				if err != nil {
					fmt.Println(ui.Failure(status.HumanizeError(err)))
				} else {
					fmt.Println(ui.Success("connected via candidate negotiation"))
				}
			}()

		case line == "/groups":
			rooms := node.Groups.List()
			if len(rooms) == 0 {
				fmt.Println(ui.Hint("no groups yet. Create one: /group create <name>"))
			} else {
				fmt.Println(ui.System("groups"))
				for _, r := range rooms {
					fmt.Printf("  - %s [%s] members=%d creator=%.12s\n", r.Name, shortID(r.ID), len(r.Members), r.Creator)
				}
			}

		case strings.HasPrefix(line, "/group "):
			handleGroupCommand(node, hist, line)

		case strings.HasPrefix(line, "/g "):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "/g "))
			space := strings.Index(rest, " ")
			if space < 0 {
				fmt.Println(ui.Hint("usage: /g <room> <message>"))
				continue
			}
			ref, text := rest[:space], strings.TrimSpace(rest[space+1:])
			if text == "" {
				fmt.Println(ui.Hint("usage: /g <room> <message>"))
				continue
			}
			room, ok := node.Groups.Resolve(ref)
			if !ok {
				fmt.Println(ui.Failure("Unknown or ambiguous room. See /groups."))
				continue
			}
			envID, err := node.SendGroup(room.ID, text, "")
			if err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
				continue
			}
			_ = hist.Add(history.ChatMessage{
				ID: envID, From: *name, JID: id.JID, LID: id.LID,
				GroupID: room.ID, Body: text, Timestamp: time.Now(), Direction: "sent",
			})
			fmt.Println(ui.Outgoing("["+room.Name+"] "+*name, text, time.Now(), timestampMode))

		case strings.HasPrefix(line, "/reply "):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "/reply "))
			space := strings.Index(rest, " ")
			if space < 0 {
				fmt.Println(ui.Hint("usage: /reply <message-id> <message>"))
				continue
			}
			ref, text := rest[:space], strings.TrimSpace(rest[space+1:])
			original, ok := hist.FindByIDPrefix(ref)
			if !ok {
				fmt.Println(ui.Failure("Message not found or ambiguous. See /history for IDs."))
				continue
			}
			if original.GroupID != "" {
				room, ok := node.Groups.Resolve(original.GroupID)
				if !ok {
					fmt.Println(ui.Failure("Original room no longer available."))
					continue
				}
				envID, err := node.SendGroup(room.ID, text, original.ID)
				if err != nil {
					fmt.Println(ui.Failure(status.HumanizeError(err)))
					continue
				}
				_ = hist.Add(history.ChatMessage{
					ID: envID, From: *name, JID: id.JID, LID: id.LID,
					GroupID: room.ID, ReplyTo: original.ID,
					Body: text, Timestamp: time.Now(), Direction: "sent",
				})
				fmt.Println(ui.Outgoing("["+room.Name+"] "+*name, renderReply(hist, original.ID, text), time.Now(), timestampMode))
			} else {
				envID := node.BroadcastReply(text, original.ID)
				_ = hist.Add(history.ChatMessage{
					ID: envID, From: *name, JID: id.JID, LID: id.LID,
					ReplyTo: original.ID, Body: text, Timestamp: time.Now(), Direction: "sent",
				})
				fmt.Println(ui.Outgoing(*name, renderReply(hist, original.ID, text), time.Now(), timestampMode))
			}

		case line == "/devices":
			fmt.Println(ui.System("this device: " + node.DeviceID + " (" + cfg.DeviceLabel + ")"))
			linked := node.LinkedDevices()
			if len(linked) == 0 {
				fmt.Println(ui.Hint("no linked devices online."))
				fmt.Println(ui.Hint("Link a second CLI: /export backup.json on this device, then /import backup.json there."))
			} else {
				fmt.Println(ui.System(fmt.Sprintf("linked devices (%d)", len(linked))))
				for _, p := range linked {
					fmt.Printf("  - %s device:%s @ %s ready=%v\n", p.Name, p.DeviceID, p.Addr, p.Ready)
				}
			}

		case line == "/status":
			phase, detail, age := node.Status.Snapshot()
			fmt.Printf("connection: %s %s (%ds ago)\n", phase, detail, int(age.Seconds()))
			fmt.Printf("queued messages: %d\n", msgQueue.Len())
			for _, p := range node.Peers() {
				last, ok := node.LastHeartbeat(p.JID)
				beat := "no heartbeat yet"
				if ok {
					beat = "last heartbeat " + last.Format("15:04:05")
				}
				fmt.Printf("  - %s [%s] %s\n", p.Name, p.Addr, beat)
			}

		case strings.HasPrefix(line, "/connect "):
			addr := strings.TrimSpace(strings.TrimPrefix(line, "/connect "))
			if err := node.ConnectTo(addr); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			} else {
				fmt.Println(ui.System("connecting to " + addr + "..."))
			}

		case strings.HasPrefix(line, "/retry "):
			addr := strings.TrimSpace(strings.TrimPrefix(line, "/retry "))
			go func() {
				if err := node.ConnectWithRetry(addr, 10, func(progress string) {
					fmt.Println(ui.Hint(progress))
				}); err != nil {
					fmt.Println(ui.Failure(status.HumanizeError(err)))
				} else {
					fmt.Println(ui.Success("connected to " + addr))
				}
			}()

		case strings.HasPrefix(line, "/history"):
			parts := strings.Fields(line)
			count := 20
			if len(parts) > 1 {
				if n, err := strconv.Atoi(parts[1]); err == nil {
					count = n
				}
			}
			messages := hist.GetLast(count)
			if len(messages) == 0 {
				fmt.Println(ui.Hint("no message history"))
			} else {
				fmt.Println(ui.System("message history (reply with /reply <id> <text>)"))
				for _, msg := range messages {
					body := msg.Body
					if msg.ReplyTo != "" {
						body = renderReply(hist, msg.ReplyTo, msg.Body)
					}
					label := msg.From
					if msg.GroupID != "" {
						label = "[" + msg.GroupID + "] " + msg.From
					}
					prefix := ""
					if msg.ID != "" {
						prefix = "[" + shortID(msg.ID) + "] "
					}
					if msg.Direction == "sent" {
						fmt.Println(prefix + ui.Outgoing(label, body, msg.Timestamp, timestampMode))
					} else {
						fmt.Println(prefix + ui.Incoming(label, body, msg.Timestamp, timestampMode))
					}
				}
			}

		case strings.HasPrefix(line, "/timestamp"):
			arg := strings.TrimSpace(strings.TrimPrefix(line, "/timestamp"))
			switch arg {
			case "relative":
				timestampMode = ui.TimestampRelative
				fmt.Println(ui.Success("timestamps: relative (e.g. 2m ago)"))
			case "absolute":
				timestampMode = ui.TimestampAbsolute
				fmt.Println(ui.Success("timestamps: absolute (HH:MM:SS)"))
			default:
				fmt.Println(ui.Hint("usage: /timestamp [relative|absolute]"))
			}

		case strings.HasPrefix(line, "/disappearing"):
			arg := strings.TrimSpace(strings.TrimPrefix(line, "/disappearing"))
			if arg == "" || arg == "off" || arg == "0" {
				hist.SetTTL(0)
				fmt.Println(ui.Success("disappearing messages: off"))
			} else {
				d, err := time.ParseDuration(arg)
				if err != nil || d <= 0 {
					fmt.Println(ui.Failure("Invalid duration. Examples: /disappearing 1h, /disappearing 24h, /disappearing off"))
				} else {
					hist.SetTTL(d)
					fmt.Println(ui.Success("disappearing messages: new messages expire after " + d.String()))
				}
			}

		case strings.HasPrefix(line, "/send "):
			filePath := strings.TrimSpace(strings.TrimPrefix(line, "/send "))
			if strings.HasPrefix(filePath, "~") {
				filePath = filepath.Join(home, filePath[1:])
			}
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				fmt.Println(ui.Failure("File not found: " + filePath))
				continue
			}
			peers := node.Peers()
			if len(peers) == 0 {
				fmt.Println(ui.Failure("No peers connected. File will not be sent."))
			} else {
				sent := false
				for _, p := range peers {
					if p.Ready {
						if err := node.SendFile(p.JID, filePath); err != nil {
							fmt.Println(ui.Failure(status.HumanizeError(err)))
						} else {
							fmt.Println(ui.FileEvent("file offer sent: " + filePath))
							sent = true
						}
						break
					}
				}
				if !sent {
					fmt.Println(ui.Failure("Encryption handshake not ready yet. Wait a moment and retry."))
				}
			}

		case strings.HasPrefix(line, "/accept "):
			fileID := strings.TrimSpace(strings.TrimPrefix(line, "/accept "))
			if err := node.AcceptFile(fileID); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			}

		case strings.HasPrefix(line, "/reject "):
			fileID := strings.TrimSpace(strings.TrimPrefix(line, "/reject "))
			if err := node.RejectFile(fileID); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			}

		case strings.HasPrefix(line, "/resume "):
			fileID := strings.TrimSpace(strings.TrimPrefix(line, "/resume "))
			if err := node.RequestResume(fileID); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			}

		case line == "/queue":
			pending := msgQueue.Pending()
			if len(pending) == 0 {
				fmt.Println(ui.Hint("message queue empty"))
			} else {
				fmt.Println(ui.System(fmt.Sprintf("queued messages (%d)", len(pending))))
				for _, m := range pending {
					fmt.Printf("  - to %s attempts=%d: %s\n", ui.Truncate(m.ToJID, 20), m.Attempts, ui.Truncate(m.Body, 60))
				}
			}

		case strings.HasPrefix(line, "/contacts"):
			parts := strings.Fields(line)
			filter := ""
			if len(parts) > 1 {
				filter = parts[1]
			}
			contactList := contactBook.List(filter)
			if len(contactList) == 0 {
				fmt.Println(ui.Hint("no contacts found"))
			} else {
				fmt.Println(ui.System("contacts (" + filter + ")"))
				for i, c := range contactList {
					fav := ""
					if c.Favorite {
						fav = "⭐"
					}
					lastSeen := "never"
					if !c.LastSeen.IsZero() {
						lastSeen = formatTimeSince(c.LastSeen)
					}
					fmt.Printf("%d. %s%s [%s] - %d msgs, last seen %s\n",
						i+1, fav, c.GetDisplayName(), c.ShortJID(), c.MessageCount, lastSeen)
					if c.LastAddress != "" {
						fmt.Printf("    address: %s\n", c.LastAddress)
					}
				}
			}

		case strings.HasPrefix(line, "/search "):
			query := strings.TrimSpace(strings.TrimPrefix(line, "/search "))
			results := contactBook.Search(query)
			if len(results) == 0 {
				fmt.Printf("no contacts found matching '%s'\n", query)
			} else {
				fmt.Println(ui.System(fmt.Sprintf("search results for '%s'", query)))
				for i, c := range results {
					fmt.Printf("%d. %s [%s] - %d msgs\n",
						i+1, c.GetDisplayName(), c.ShortJID(), c.MessageCount)
				}
			}

		case strings.HasPrefix(line, "/alias "):
			parts := strings.Fields(line)
			if len(parts) < 3 {
				fmt.Println(ui.Hint("usage: /alias <jid> <display_name>"))
			} else {
				jid := parts[1]
				alias := strings.Join(parts[2:], " ")
				if err := contactBook.SetDisplayName(jid, alias); err != nil {
					fmt.Println(ui.Failure(status.HumanizeError(err)))
				} else {
					fmt.Printf("set alias for %s: %s\n", jid, alias)
				}
			}

		case strings.HasPrefix(line, "/favorite "):
			jid := strings.TrimSpace(strings.TrimPrefix(line, "/favorite "))
			if err := contactBook.SetFavorite(jid, true); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			} else {
				fmt.Println(ui.Success("added to favorites"))
			}

		case strings.HasPrefix(line, "/block "):
			jid := strings.TrimSpace(strings.TrimPrefix(line, "/block "))
			if err := contactBook.SetBlocked(jid, true); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			} else {
				fmt.Println(ui.Success("contact blocked"))
			}

		case strings.HasPrefix(line, "/unblock "):
			jid := strings.TrimSpace(strings.TrimPrefix(line, "/unblock "))
			if err := contactBook.SetBlocked(jid, false); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			} else {
				fmt.Println(ui.Success("contact unblocked"))
			}

		case strings.HasPrefix(line, "/contact "):
			jid := strings.TrimSpace(strings.TrimPrefix(line, "/contact "))
			if c, ok := contactBook.Get(jid); ok {
				fmt.Println(ui.System("contact details"))
				fmt.Printf("Name: %s\n", c.Name)
				if c.DisplayName != "" {
					fmt.Printf("Alias: %s\n", c.DisplayName)
				}
				fmt.Printf("JID: %s\n", c.JID)
				fmt.Printf("LID: %s\n", c.ShortJID())
				fmt.Printf("Messages: %d\n", c.MessageCount)
				fmt.Printf("First met: %s\n", c.FirstMet.Format("2006-01-02 15:04:05"))
				fmt.Printf("Last seen: %s\n", formatTimeSince(c.LastSeen))
				if c.LastAddress != "" {
					fmt.Printf("Last address: %s\n", c.LastAddress)
				}
				if c.Favorite {
					fmt.Println("⭐ Favorite")
				}
				if c.Blocked {
					fmt.Println("🚫 Blocked")
				}
				if len(c.Tags) > 0 {
					fmt.Printf("Tags: %s\n", strings.Join(c.Tags, ", "))
				}
				if c.Notes != "" {
					fmt.Printf("Notes: %s\n", c.Notes)
				}
			} else {
				fmt.Println(ui.Failure("Contact not found. Use /search to find it."))
			}

		case strings.HasPrefix(line, "/verify "), strings.HasPrefix(line, "/safety "):
			var jid string
			if strings.HasPrefix(line, "/verify ") {
				jid = strings.TrimSpace(strings.TrimPrefix(line, "/verify "))
			} else {
				jid = strings.TrimSpace(strings.TrimPrefix(line, "/safety "))
			}
			showSafetyNumber(contactBook, jid)
			audit.Log("verify.show", *name, "safety number displayed for "+shortID(jid))

		case line == "/test-nat":
			fmt.Println(ui.System("testing NAT type using STUN server " + *stunServer + "..."))
			go func() {
				res, err := nat.DetectNATType(*stunServer, 5*time.Second)
				if err != nil {
					fmt.Println(ui.Failure("NAT test failed: " + status.HumanizeError(err)))
				} else {
					fmt.Println(ui.Success(fmt.Sprintf("NAT Test Result: %s (Public IP: %s, Latency: %v, Compatibility: %s)", res.NATType, res.PublicIP, res.Latency, res.Compatibility)))
				}
			}()

		case strings.HasPrefix(line, "/trust "):
			jid := strings.TrimSpace(strings.TrimPrefix(line, "/trust "))
			if c, ok := contactBook.Get(jid); ok {
				c.Notes = strings.TrimSpace(c.Notes + " [trusted]")
				_ = contactBook.SetFavorite(jid, true)
				fmt.Println(ui.Success("Contact " + c.GetDisplayName() + " (" + shortID(jid) + ") explicitly trusted."))
			} else {
				contactBook.Add(jid, shortID(jid))
				fmt.Println(ui.Success("Added and trusted contact " + shortID(jid)))
			}

		case strings.HasPrefix(line, "/ephemeral"):
			arg := strings.TrimSpace(strings.TrimPrefix(line, "/ephemeral"))
			if arg == "" || arg == "off" || arg == "0" {
				hist.SetTTL(0)
				fmt.Println(ui.Success("disappearing messages: off"))
			} else {
				d, err := time.ParseDuration(arg)
				if err != nil || d <= 0 {
					fmt.Println(ui.Failure("Invalid duration. Examples: /ephemeral 1h, /ephemeral 24h, /ephemeral off"))
				} else {
					hist.SetTTL(d)
					fmt.Println(ui.Success("disappearing messages: new messages expire after " + d.String()))
				}
			}

		case line == "/link-device":
			fmt.Println(ui.System("=== Link a Secondary Device ==="))
			fmt.Println("To link a second CLI (laptop, server) with the same identity:")
			fmt.Println("1) Run on this device:   /export backup.json")
			fmt.Println("2) Copy backup.json to your second device safely.")
			fmt.Println("3) Run on second device: /import backup.json")
			fmt.Println("Both devices will share identity while maintaining unique device IDs.")

		case strings.HasPrefix(line, "/export-identity "):
			dest := strings.TrimSpace(strings.TrimPrefix(line, "/export-identity "))
			fmt.Print("Export password (min 8 chars): ")
			password := readPasswordLine()
			payload := backup.Payload{PrivateKeyHex: id.RawPrivateHex()}
			if err := backup.Export(dest, password, payload); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			} else {
				fmt.Println(ui.Success("identity exported to " + dest))
				audit.Log("identity.export_only", *name, "identity exported to "+dest)
			}

		case strings.HasPrefix(line, "/import-identity "):
			src := strings.TrimSpace(strings.TrimPrefix(line, "/import-identity "))
			fmt.Print("Export password: ")
			password := readPasswordLine()
			payload, err := backup.Import(src, password)
			if err != nil || payload.PrivateKeyHex == "" {
				fmt.Println(ui.Failure("Invalid identity backup or wrong password."))
				continue
			}
			if _, err := identity.RestoreFromHex(payload.PrivateKeyHex, identity.DefaultIdentityPath()); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			} else {
				fmt.Println(ui.Success("identity restored. Restart korik to use the restored identity."))
				audit.Log("identity.import_only", *name, "identity imported from "+src)
			}

		case line == "/check-update":
			fmt.Println(ui.System("checking for updates..."))
			go func() {
				info, err := updateChecker.ForceCheck()
				if err != nil {
					fmt.Println(ui.Failure("Update check failed: " + status.HumanizeError(err)))
				} else if info != nil && info.HasUpdate {
					fmt.Println(ui.Success(fmt.Sprintf("Update available: %s (current: %s). Download at %s", info.Version, info.CurrentVersion, info.NotesURL)))
				} else {
					fmt.Println(ui.Success("You are running the latest version of korik."))
				}
			}()

		case strings.HasPrefix(line, "/export-data "):
			line = strings.Replace(line, "/export-data ", "/export ", 1)
			dest := strings.TrimSpace(strings.TrimPrefix(line, "/export "))
			fmt.Print("Backup password (min 8 chars): ")
			password := readPasswordLine()
			payload, err := buildBackupPayload(identity.DefaultIdentityPath())
			if err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
				continue
			}
			if err := backup.Export(dest, password, payload); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			} else {
				fmt.Println(ui.Success("backup exported to " + dest))
				audit.Log("identity.export", *name, "backup exported to "+dest)
			}

		case strings.HasPrefix(line, "/import-data "):
			line = strings.Replace(line, "/import-data ", "/import ", 1)
			src := strings.TrimSpace(strings.TrimPrefix(line, "/import "))
			fmt.Print("Backup password: ")
			password := readPasswordLine()
			payload, err := backup.Import(src, password)
			if err != nil {
				fmt.Println(ui.Failure("Wrong password or corrupted backup."))
				continue
			}
			if err := restoreBackupPayload(payload); err != nil {
				fmt.Println(ui.Failure(status.HumanizeError(err)))
			} else {
				fmt.Println(ui.Success("backup restored. Restart korik to use the restored identity."))
				audit.Log("identity.import", *name, "backup imported from "+src)
			}

		default:
			// Regular chat message: broadcast, queue when offline.
			peers := node.Peers()
			ready := 0
			for _, p := range peers {
				if p.Ready {
					ready++
				}
			}
			var envID string
			if ready == 0 {
				// Queue for all known contacts so it flushes on reconnect.
				queued := 0
				for _, c := range contactBook.List("") {
					_ = msgQueue.Enqueue(queue.OutgoingMessage{
						ToJID: c.JID, Body: line, CreatedAt: time.Now(),
					})
					queued++
				}
				if queued == 0 {
					fmt.Println(ui.Failure("No peers connected yet. Message saved to history only."))
				} else {
					fmt.Println(ui.Hint(fmt.Sprintf("No peers online. Queued for %d contact(s), will send on reconnect.", queued)))
				}
			} else {
				envID = node.Broadcast(line)
			}
			_ = hist.Add(history.ChatMessage{
				ID: envID, From: *name, JID: id.JID, LID: id.LID,
				SenderDevice: node.DeviceID,
				Body:         line, Timestamp: time.Now(), Direction: "sent",
			})
			fmt.Println(ui.Outgoing(*name, line, time.Now(), timestampMode))
		}

		lastInput = time.Now()
	}
}

func printBanner(name, jid, lid string, port, dport, queued int) {
	fmt.Println(ui.System("korik p2p chat (end-to-end encrypted)"))
	fmt.Printf("name: %s\n", name)
	fmt.Printf("JID : %s\n", jid)
	fmt.Printf("LID : %s\n", lid)
	fmt.Printf("listen tcp: %d | discovery udp: %d\n", port, dport)
	if queued > 0 {
		fmt.Println(ui.Hint(fmt.Sprintf("%d queued message(s) will be delivered on reconnect. See /queue.", queued)))
	}
	fmt.Println(ui.Hint("Type /help for commands. Tab completes, Up/Down recalls history."))
}

func printHelp() {
	fmt.Println(ui.System("commands"))
	fmt.Println("  Peers & connection:")
	fmt.Println("    /peers, /devices, /candidates, /status, /test-nat")
	fmt.Println("    /connect <ip:port>, /retry <ip:port>, /link-device")
	fmt.Println("    /connect-candidates <bundle> (robust: LAN → IPv6 → public → punching)")
	fmt.Println("  Messaging:")
	fmt.Println("    /history [n], /queue, /timestamp [relative|absolute]")
	fmt.Println("    /disappearing [1h|24h|off] (alias /ephemeral), /reply <id> <text>")
	fmt.Println("  Groups:")
	fmt.Println("    /groups, /group create|invite|leave|rekey, /g <room> <text>")
	fmt.Println("  Files:")
	fmt.Println("    /send <path>, /accept <id>, /reject <id>, /resume <id>")
	fmt.Println("  Contacts:")
	fmt.Println("    /contacts [filter], /search <query> (fuzzy), /contact <jid>")
	fmt.Println("    /alias, /favorite, /block, /unblock, /trust <jid>")
	fmt.Println("  Security & backup:")
	fmt.Println("    /verify <jid>, /safety <jid>, /export <path>, /import <path>")
	fmt.Println("    /export-identity <path>, /import-identity <path>")
	fmt.Println("    /export-data <path>, /import-data <path>")
	fmt.Println("  App: /check-update, /help, /quit")
}

func printCandidates(node *peer.Node) {
	cands := node.LocalCandidates()
	if len(cands) == 0 {
		fmt.Println(ui.Hint("no candidates yet (network interfaces unavailable)"))
		return
	}
	fmt.Println(ui.System(fmt.Sprintf("candidates (%d, priority order)", len(cands))))
	for _, c := range cands {
		fmt.Printf("  - [%s] %s %s %s pri=%d iface=%s\n",
			nat.StepFor(c), c.Type, c.Transport, c.Address(), c.Priority, c.Interface)
	}
	bundle := node.BundleForShare()
	fmt.Println(ui.Hint("share bundle (peer runs /connect-candidates <bundle>):"))
	fmt.Println(bundle)
	fmt.Println(ui.Hint("fingerprint: " + nat.Fingerprint(nat.Bundle{Candidates: cands})))
}

// shortID renders the first 8 chars of an envelope or room ID.
func shortID(full string) string {
	if len(full) <= 8 {
		return full
	}
	return full[:8]
}

// renderReply formats a reply with its quoted original preview.
func renderReply(hist *history.History, replyTo, body string) string {
	if replyTo == "" {
		return body
	}
	original, ok := hist.FindByIDPrefix(replyTo)
	preview := shortID(replyTo)
	if ok {
		preview = ui.Truncate(original.Body, 40)
		if original.From != "" {
			preview = original.From + ": " + preview
		}
	}
	return "↩ " + preview + "\n" + body
}

func handleGroupCommand(node *peer.Node, hist *history.History, line string) {
	args := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "/group ")))
	if len(args) == 0 {
		fmt.Println(ui.Hint("usage: /group create|invite|leave|rekey ... (see /help)"))
		return
	}
	switch args[0] {
	case "create":
		if len(args) < 2 {
			fmt.Println(ui.Hint("usage: /group create <name>"))
			return
		}
		room, err := node.CreateRoom(strings.Join(args[1:], " "))
		if err != nil {
			fmt.Println(ui.Failure(status.HumanizeError(err)))
			return
		}
		fmt.Println(ui.Success("group created: " + room.Name + " [" + shortID(room.ID) + "]"))
		fmt.Println(ui.Hint("Invite members: /group invite " + shortID(room.ID) + " <jid>"))
		audit.Log("group.create", node.Name, "room "+shortID(room.ID))
	case "invite":
		if len(args) < 3 {
			fmt.Println(ui.Hint("usage: /group invite <room> <jid>"))
			return
		}
		room, ok := node.Groups.Resolve(args[1])
		if !ok {
			fmt.Println(ui.Failure("Unknown or ambiguous room. See /groups."))
			return
		}
		if err := node.InviteToRoom(room.ID, args[2]); err != nil {
			fmt.Println(ui.Failure(status.HumanizeError(err)))
			return
		}
		fmt.Println(ui.Success("invited " + shortID(args[2]) + " to " + room.Name))
		audit.Log("group.invite", node.Name, "room "+shortID(room.ID)+" member "+shortID(args[2]))
	case "leave":
		if len(args) < 2 {
			fmt.Println(ui.Hint("usage: /group leave <room>"))
			return
		}
		room, ok := node.Groups.Resolve(args[1])
		if !ok {
			fmt.Println(ui.Failure("Unknown or ambiguous room. See /groups."))
			return
		}
		if err := node.LeaveRoom(room.ID); err != nil {
			fmt.Println(ui.Failure(status.HumanizeError(err)))
			return
		}
		fmt.Println(ui.Success("left group " + room.Name))
	case "rekey":
		if len(args) < 2 {
			fmt.Println(ui.Hint("usage: /group rekey <room>"))
			return
		}
		room, ok := node.Groups.Resolve(args[1])
		if !ok {
			fmt.Println(ui.Failure("Unknown or ambiguous room. See /groups."))
			return
		}
		if err := node.RekeyRoom(room.ID); err != nil {
			fmt.Println(ui.Failure(status.HumanizeError(err)))
			return
		}
		fmt.Println(ui.Success("group key rotated for " + room.Name))
		audit.Log("group.rekey", node.Name, "room "+shortID(room.ID))
	default:
		fmt.Println(ui.Hint("usage: /group create|invite|leave|rekey ... (see /help)"))
	}
}

func showSafetyNumber(book *contacts.ContactBook, jid string) {
	c, ok := book.Get(jid)
	pubHex := ""
	if ok {
		// JID format is "korik:<hex>".
		if parts := strings.SplitN(c.JID, ":", 2); len(parts) == 2 {
			pubHex = parts[1]
		}
	} else {
		// Allow raw JID input too.
		if parts := strings.SplitN(jid, ":", 2); len(parts) == 2 {
			pubHex = parts[1]
		} else {
			pubHex = jid
		}
	}
	number, err := verify.SafetyNumber(pubHex)
	if err != nil {
		fmt.Println(ui.Failure("Invalid key for safety number. Use a full JID from /contacts."))
		return
	}
	name := jid
	if ok {
		name = c.GetDisplayName()
	}
	fmt.Println(ui.System("safety number for " + name))
	fmt.Println("  " + number)
	fmt.Println(ui.Hint("Compare this number over a trusted channel (call/in person)."))
	fmt.Println(ui.Hint("Short code: " + verify.ShortCode(pubHex)))
}

func readPasswordLine() string {
	// No-echo password prompt when stdin is a terminal (CLI-only).
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("(input hidden) ")
		password, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err == nil {
			return strings.TrimSpace(string(password))
		}
	}
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func buildBackupPayload(identityPath string) (backup.Payload, error) {
	data, err := os.ReadFile(identityPath)
	if err != nil {
		return backup.Payload{}, err
	}
	var stored struct {
		PrivateKeyHex string `json:"private_key_hex"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return backup.Payload{}, err
	}
	home, _ := os.UserHomeDir()
	var cfg, cts, hist json.RawMessage
	if b, err := os.ReadFile(filepath.Join(home, ".korik", "config.json")); err == nil {
		cfg = b
	}
	if b, err := os.ReadFile(filepath.Join(home, ".korik", "contacts.json")); err == nil {
		cts = b
	}
	if b, err := os.ReadFile(filepath.Join(home, ".korik", "history.json")); err == nil {
		hist = b
	}
	return backup.Payload{
		PrivateKeyHex: stored.PrivateKeyHex,
		Config:        cfg,
		Contacts:      cts,
		History:       hist,
	}, nil
}

func restoreBackupPayload(payload backup.Payload) error {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".korik")
	if payload.PrivateKeyHex == "" {
		return fmt.Errorf("backup has no identity")
	}
	if _, err := identity.RestoreFromHex(payload.PrivateKeyHex, filepath.Join(base, "identity.json")); err != nil {
		return err
	}
	if len(payload.Contacts) > 0 {
		_ = os.WriteFile(filepath.Join(base, "contacts.json"), payload.Contacts, 0o600)
	}
	if len(payload.History) > 0 {
		_ = os.WriteFile(filepath.Join(base, "history.json"), payload.History, 0o600)
	}
	// CLI-only linked device: keep a distinct DeviceID so two CLIs with the
	// same JID recognize each other instead of skipping as self.
	if len(payload.Config) > 0 {
		var imported config.Config
		if err := json.Unmarshal(payload.Config, &imported); err == nil {
			current, _ := config.Load(filepath.Join(base, "config.json"))
			if current == nil {
				current = config.DefaultConfig()
			}
			// Preserve name/port choices from backup, but always use a fresh device ID.
			current.Name = imported.Name
			if imported.Port != 0 {
				// Avoid port clash when both devices run on one host.
				if imported.Port == current.Port {
					current.Port = imported.Port + 1
				} else {
					current.Port = imported.Port
				}
			}
			current.EnsureDeviceID()
			if current.DeviceID == imported.DeviceID {
				current.DeviceID = ""
				current.EnsureDeviceID()
			}
			_ = current.Save(filepath.Join(base, "config.json"))
		} else {
			_ = os.WriteFile(filepath.Join(base, "config.json"), payload.Config, 0o600)
		}
	}
	return nil
}

func formatTimeSince(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		mins := int(duration.Minutes())
		return fmt.Sprintf("%d min ago", mins)
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		return fmt.Sprintf("%d hour ago", hours)
	case duration < 7*24*time.Hour:
		days := int(duration.Hours() / 24)
		return fmt.Sprintf("%d day ago", days)
	default:
		return t.Format("2006-01-02")
	}
}
