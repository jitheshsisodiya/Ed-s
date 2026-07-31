package agent

import "testing"

// The words a user reads are the product's contract with them, so the
// mapping from path state to language is tested directly rather than left
// to whichever frontend renders it.
func TestDescribeQuality(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		latency int
		want    string
	}{
		{"direct and instant", "direct", 12, QualityExcellent},
		{"direct at the excellent boundary", "direct", 49, QualityExcellent},
		{"direct just past it", "direct", 50, QualityGood},
		{"direct and responsive", "direct", 120, QualityGood},
		{"direct but sluggish", "direct", 150, QualityLimited},
		{"direct and very slow", "direct", 800, QualityLimited},

		// A direct path exists but has not been probed yet. Reporting
		// "Offline" here would be a lie the user can see through, since
		// traffic is already flowing.
		{"direct, not yet probed", "direct", -1, QualityGood},

		// A relay always costs an extra hop, however fast it measures.
		{"relayed but fast", "relay", 8, QualityLimited},
		{"relayed and slow", "relay", 300, QualityLimited},

		{"offline", "offline", -1, QualityOffline},
		{"still connecting", "connecting", -1, QualityOffline},
		{"unknown mode", "", -1, QualityOffline},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeQuality(tc.mode, tc.latency); got != tc.want {
				t.Fatalf("describeQuality(%q, %d) = %q, want %q", tc.mode, tc.latency, got, tc.want)
			}
		})
	}
}

// Whatever the inputs, the user must never be shown a word we did not
// choose deliberately.
func TestQualityIsAlwaysOneOfFourWords(t *testing.T) {
	allowed := map[string]bool{
		QualityExcellent: true,
		QualityGood:      true,
		QualityLimited:   true,
		QualityOffline:   true,
	}

	for _, mode := range []string{"direct", "relay", "offline", "connecting", "", "nonsense"} {
		for _, latency := range []int{-1, 0, 49, 50, 149, 150, 10000} {
			got := describeQuality(mode, latency)
			if !allowed[got] {
				t.Fatalf("describeQuality(%q, %d) returned %q, which is not a sanctioned word", mode, latency, got)
			}
		}
	}
}
