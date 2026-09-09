package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	CurrentVersion      ProtocolVersion = "1.0.0"
	MinSupportedVersion ProtocolVersion = "1.0.0"
)

type ProtocolVersion string

type Version struct {
	Major int
	Minor int
	Patch int
}

var CompatibilityMatrix = map[string]bool{
	"1.0.0": true,
}

func ParseVersion(s string) (*Version, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid version format: %s", s)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid major version: %s", parts[0])
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid minor version: %s", parts[1])
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid patch version: %s", parts[2])
	}
	return &Version{Major: major, Minor: minor, Patch: patch}, nil
}

func Compare(a, b *Version) int {
	if a.Major != b.Major {
		if a.Major > b.Major {
			return 1
		}
		return -1
	}
	if a.Minor != b.Minor {
		if a.Minor > b.Minor {
			return 1
		}
		return -1
	}
	if a.Patch != b.Patch {
		if a.Patch > b.Patch {
			return 1
		}
		return -1
	}
	return 0
}

func IsCompatible(peerVersion *Version) bool {
	current, err := ParseVersion(string(CurrentVersion))
	if err != nil {
		return false
	}
	min, err := ParseVersion(string(MinSupportedVersion))
	if err != nil {
		return false
	}
	return Compare(peerVersion, min) >= 0 && IsMajorMatch(current, peerVersion)
}

func NegotiateVersion(peerVersion *Version) (*Version, string) {
	current, _ := ParseVersion(string(CurrentVersion))
	min, _ := ParseVersion(string(MinSupportedVersion))

	if !IsMajorMatch(current, peerVersion) {
		return nil, ""
	}

	if Compare(peerVersion, min) < 0 {
		return nil, ""
	}

	if peerVersion.Major == current.Major && peerVersion.Minor == current.Minor && peerVersion.Patch == current.Patch {
		return current, "normal"
	}

	if peerVersion.Major == current.Major && peerVersion.Minor == current.Minor {
		if peerVersion.Patch < current.Patch {
			return peerVersion, "normal"
		}
		return current, "normal"
	}

	if peerVersion.Major == current.Major && peerVersion.Minor < current.Minor {
		return peerVersion, "compat"
	}

	if peerVersion.Major == current.Major && peerVersion.Minor > current.Minor {
		return current, "compat"
	}

	return current, "normal"
}

func IsMajorMatch(a, b *Version) bool {
	return a.Major == b.Major
}

func HandshakeVersion() string {
	return string(CurrentVersion)
}

func VersionMismatchError(peerVersion *Version) error {
	minVer, _ := ParseVersion(string(MinSupportedVersion))
	return fmt.Errorf("version mismatch: peer version %s, minimum required version %s",
		formatVersion(peerVersion), formatVersion(minVer))
}

func formatVersion(v *Version) string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}
