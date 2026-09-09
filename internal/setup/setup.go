package setup

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"korik/internal/config"
	"korik/internal/validate"
)

// DetectConnectionMode suggests LAN or Internet based on local interfaces.
// Returns "lan" when a private IPv4 address is present, otherwise "internet".
func DetectConnectionMode() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "lan"
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip.To4() != nil && ip.IsPrivate() {
				return "lan"
			}
		}
	}
	return "internet"
}

// IsFirstRun reports true when neither config nor identity exists yet.
func IsFirstRun(configPath, identityPath string) bool {
	if _, err := os.Stat(configPath); err == nil {
		return false
	}
	if _, err := os.Stat(identityPath); err == nil {
		return false
	}
	return true
}

// RunWizard runs an interactive first-run setup: nickname, mode suggestion,
// and persists the config. It returns the nickname and whether internet
// mode was selected.
func RunWizard(cfg *config.Config, reader *bufio.Reader) (string, bool) {
	fmt.Println("=== Welcome to korik ===")
	fmt.Println("First-time setup. Defaults work for most users.")
	fmt.Println()

	nickname := strings.TrimSpace(cfg.Name)
	for {
		if nickname == "" || nickname == "korik-user" {
			fmt.Print("Choose a nickname [korik-user]: ")
			if line, err := reader.ReadString('\n'); err == nil {
				line = strings.TrimSpace(line)
				if line != "" {
					nickname = line
				} else {
					nickname = "korik-user"
				}
			}
		} else {
			fmt.Printf("Nickname [%s] (press Enter to keep): ", nickname)
			if line, err := reader.ReadString('\n'); err == nil {
				line = strings.TrimSpace(line)
				if line != "" {
					nickname = line
				}
			}
		}
		if err := validate.Nickname(nickname); err != nil {
			fmt.Printf("Invalid nickname (%v). Please try again.\n", err)
			nickname = ""
			continue
		}
		break
	}

	suggested := DetectConnectionMode()
	fmt.Printf("Detected network: %s\n", suggested)
	fmt.Println("Connection mode:")
	fmt.Println("  1) LAN / WiFi hotspot (auto-discovery, recommended)")
	fmt.Println("  2) Internet (STUN + UDP hole punching, for different networks)")
	defaultChoice := "1"
	if suggested == "internet" {
		defaultChoice = "2"
	}
	fmt.Printf("Select [1/2, default %s]: ", defaultChoice)
	useInternet := false
	if line, err := reader.ReadString('\n'); err == nil {
		line = strings.TrimSpace(line)
		if line == "" {
			line = defaultChoice
		}
		useInternet = line == "2"
	}

	cfg.Name = nickname
	cfg.InternetMode = useInternet

	fmt.Println()
	if useInternet {
		fmt.Println("Internet mode selected. Share your public address with your peer after start.")
	} else {
		fmt.Println("LAN mode selected. Peers on the same WiFi will be found automatically (<1s).")
		fmt.Println("Tip: if discovery fails, use /connect <ip:port> manually.")
	}
	fmt.Println()

	return nickname, useInternet
}
