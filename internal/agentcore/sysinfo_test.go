package agentcore

import (
	"strings"
	"testing"
)

func TestParseMeminfo(t *testing.T) {
	sample := `MemTotal:       16384256 kB
MemFree:         1048576 kB
MemAvailable:    8388608 kB
Buffers:          204800 kB
Cached:          4096000 kB
`
	total, avail := parseMeminfo(strings.NewReader(sample))
	if want := int64(16384256) * 1024; total != want {
		t.Errorf("MemTotal = %d, want %d", total, want)
	}
	if want := int64(8388608) * 1024; avail != want {
		t.Errorf("MemAvailable = %d, want %d", avail, want)
	}
}

func TestParseMeminfoMissingFields(t *testing.T) {
	// No MemAvailable line (very old kernels) — total parses, available stays zero.
	total, avail := parseMeminfo(strings.NewReader("MemTotal: 2048 kB\nMemFree: 1024 kB\n"))
	if total != 2048*1024 {
		t.Errorf("MemTotal = %d, want %d", total, 2048*1024)
	}
	if avail != 0 {
		t.Errorf("MemAvailable = %d, want 0", avail)
	}
}

func TestParseCaddyVersion(t *testing.T) {
	cases := map[string]string{
		"v2.7.6 h1:abcdef":            "2.7.6",
		"v2.8.4\n":                    "2.8.4",
		"2.9.0":                       "2.9.0",
		"":                            "",
		"  v2.7.6  (built locally)\n": "2.7.6",
	}
	for in, want := range cases {
		if got := parseCaddyVersion(in); got != want {
			t.Errorf("parseCaddyVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
