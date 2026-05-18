package brainapi

import (
	"crypto/md5" //nolint:gosec // MD5 used for deterministic bucketing, not crypto.
	"encoding/binary"
	"strings"

	"github.com/bogdanfinn/tls-client/profiles"
)

// BrowserProfile identifies the TLS/JA3 + HTTP/2 fingerprint to impersonate.
// The string names are stable across SDK versions even when the underlying
// tls-client constants are renamed — ParseProfile maps user input here.
type BrowserProfile string

const (
	ProfileChrome131  BrowserProfile = "chrome131" // production-parity with the production bridge
	ProfileChrome133  BrowserProfile = "chrome133" // tls-client's current default
	ProfileChrome144  BrowserProfile = "chrome144"
	ProfileChrome146  BrowserProfile = "chrome146"
	ProfileSafari16   BrowserProfile = "safari16"
	ProfileSafariIOS  BrowserProfile = "safari-ios" // alias for Safari_IOS_18_0
	ProfileFirefox132 BrowserProfile = "firefox132"
	ProfileFirefox147 BrowserProfile = "firefox147"
)

// DefaultProfile is the production-recommended starting point. We keep this
// at chrome131 (mapped to Chrome_131_PSK internally) for parity with the
// production bridge — migrating an account from TS to Go preserves its
// fingerprint identity.
const DefaultProfile = ProfileChrome131

// subProfiles is the deterministic-rotation pool for secondary accounts. Keeping
// it stable means MD5(email) -> profile mapping doesn't drift between SDK
// releases.
var subProfiles = []BrowserProfile{
	ProfileChrome131,
	ProfileChrome133,
	ProfileChrome144,
	ProfileSafariIOS,
	ProfileFirefox132,
}

// ProfileForEmail picks a profile deterministically from an email address by
// taking the low 64 bits of md5(email) modulo len(subProfiles). MD5 is used
// here purely as a uniform hash — it is NOT load-bearing for security.
func ProfileForEmail(email string) BrowserProfile {
	if email == "" {
		return DefaultProfile
	}
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email)))) //nolint:gosec
	idx := binary.BigEndian.Uint64(sum[:8]) % uint64(len(subProfiles))
	return subProfiles[idx]
}

// tlsClientProfile maps the public profile names to bogdanfinn/tls-client's
// internal profile values. Adding a new profile = one line here + one const
// above.
func tlsClientProfile(p BrowserProfile) profiles.ClientProfile {
	switch p {
	case ProfileChrome131:
		return profiles.Chrome_131_PSK
	case ProfileChrome133:
		return profiles.Chrome_133
	case ProfileChrome144:
		return profiles.Chrome_144
	case ProfileChrome146:
		return profiles.Chrome_146
	case ProfileSafari16:
		return profiles.Safari_16_0
	case ProfileSafariIOS:
		return profiles.Safari_IOS_18_0
	case ProfileFirefox132:
		return profiles.Firefox_132
	case ProfileFirefox147:
		return profiles.Firefox_147
	default:
		return profiles.Chrome_131_PSK
	}
}

// ParseProfile resolves a user-facing string ("chrome131" / "safari17" /
// "auto:user@x.com") into a BrowserProfile. Unknown strings fall back to
// DefaultProfile.
func ParseProfile(s string) BrowserProfile {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "auto:") {
		return ProfileForEmail(strings.TrimPrefix(s, "auto:"))
	}
	for _, p := range subProfiles {
		if string(p) == s {
			return p
		}
	}
	if s == "" {
		return DefaultProfile
	}
	return DefaultProfile
}
