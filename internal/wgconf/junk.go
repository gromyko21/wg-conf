package wgconf

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
)

// JunkParams are obfuscation parameters for AmneziaWG-compatible clients.
type JunkParams struct {
	Jc   int `json:"jc"`
	Jmin int `json:"jmin"`
	Jmax int `json:"jmax"`
}

// GenerateJunkParams returns random but valid Jc/Jmin/Jmax values.
func GenerateJunkParams() (JunkParams, error) {
	jc, err := randInt(2, 7)
	if err != nil {
		return JunkParams{}, err
	}
	jmin, err := randInt(8, 48)
	if err != nil {
		return JunkParams{}, err
	}
	// Jmax must be greater than Jmin; keep sizes realistic for junk packets.
	maxDelta, err := randInt(16, 96)
	if err != nil {
		return JunkParams{}, err
	}
	jmax := jmin + maxDelta
	if jmax > 128 {
		jmax = 128
	}
	if jmax <= jmin {
		jmax = jmin + 8
	}

	return JunkParams{Jc: jc, Jmin: jmin, Jmax: jmax}, nil
}

func randInt(min, max int) (int, error) {
	if min > max {
		return 0, fmt.Errorf("invalid range")
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	n := binary.BigEndian.Uint64(b[:])
	return min + int(n%uint64(max-min+1)), nil
}

func (j JunkParams) lines() string {
	return fmt.Sprintf("Jc = %d\nJmin = %d\nJmax = %d\n", j.Jc, j.Jmin, j.Jmax)
}

// HasJunkParams reports whether config already contains junk packet settings.
func HasJunkParams(config string) bool {
	for _, line := range strings.Split(config, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Jc") && strings.Contains(trimmed, "=") {
			return true
		}
	}
	return false
}

// EnsureJunkParams adds Jc/Jmin/Jmax to [Interface] if they are missing.
func EnsureJunkParams(config string) (string, JunkParams, error) {
	if HasJunkParams(config) {
		return config, JunkParams{}, nil
	}
	junk, err := GenerateJunkParams()
	if err != nil {
		return "", JunkParams{}, err
	}
	updated, err := injectJunkParams(config, junk)
	if err != nil {
		return "", JunkParams{}, err
	}
	return updated, junk, nil
}

func injectJunkParams(config string, junk JunkParams) (string, error) {
	lines := strings.Split(config, "\n")
	var out []string
	inInterface := false
	inserted := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "[Peer]" && inInterface && !inserted {
			out = append(out, strings.TrimRight(junk.lines(), "\n"))
			out = append(out, "")
			inserted = true
			inInterface = false
		}

		out = append(out, line)

		if trimmed == "[Interface]" {
			inInterface = true
		}
		if trimmed == "[Peer]" {
			inInterface = false
		}
	}

	if !inserted {
		return "", fmt.Errorf("invalid client config: [Interface] section not found")
	}

	result := strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
	return result, nil
}
