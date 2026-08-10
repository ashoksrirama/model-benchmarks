package runtime

import "testing"

// TestIsHighBandwidthInstance pins the >=50 Gbps classification for the GPU
// fleet, sourced from AWS EC2 accelerated-computing network specs. The small
// bursty G instances (baseline 2.5-10 Gbps) must be standard; the big multi-GPU
// nodes and the p-series must be high-bandwidth.
func TestIsHighBandwidthInstance(t *testing.T) {
	high := []string{
		"g5.24xlarge", "g5.48xlarge", "g6.24xlarge", "g6.48xlarge",
		"g6e.12xlarge", "g6e.24xlarge", "g6e.48xlarge",
		"g7e.2xlarge", "g7e.48xlarge",
		"p4d.24xlarge", "p4de.24xlarge", "p5.48xlarge", "p5e.48xlarge", "p5en.48xlarge",
	}
	standard := []string{
		"g5.xlarge", "g5.2xlarge", "g5.4xlarge", "g5.12xlarge", // 40 Gbps < 50
		"g6.xlarge", "g6.2xlarge", "g6.4xlarge", "g6.12xlarge",
		"g6e.xlarge", "g6e.2xlarge", "g6e.4xlarge", "g6e.16xlarge", // 35 Gbps
		"gr6.4xlarge", "gr6.8xlarge",
		"unknown.instance", "", // unknown ⇒ standard (safe direction)
	}
	for _, n := range high {
		if !IsHighBandwidthInstance(n) {
			t.Errorf("%s should be high-bandwidth (>=%d Gbps)", n, highBandwidthGbpsThreshold)
		}
	}
	for _, n := range standard {
		if IsHighBandwidthInstance(n) {
			t.Errorf("%s should be standard-bandwidth (<%d Gbps)", n, highBandwidthGbpsThreshold)
		}
	}
}

// TestStreamerChunkBytesize: high-BW instances get AWS's 4 GiB chunk; standard
// instances get "" (inherit the streamer's 8 MiB object-storage default).
func TestStreamerChunkBytesize(t *testing.T) {
	if got := StreamerChunkBytesize("p5.48xlarge"); got != "4294967296" {
		t.Errorf("high-BW chunk = %q, want 4294967296", got)
	}
	if got := StreamerChunkBytesize("g6.2xlarge"); got != "" {
		t.Errorf("standard-BW chunk = %q, want \"\" (default)", got)
	}
}

// TestStreamerConcurrency covers the bandwidth-aware resolver end to end.
func TestStreamerConcurrency(t *testing.T) {
	gib := int64(1024 * 1024 * 1024)
	cases := []struct {
		name             string
		explicit         int
		sizeBytes        int64
		instance         string
		want             int
	}{
		{"explicit always wins", 12, 67 * gib, "p5.48xlarge", 12},
		{"standard → 32 regardless of size", 0, 67 * gib, "g6.2xlarge", 32},
		{"high-BW size-derived ceil(67/4)=17", 0, 67 * gib, "p5.48xlarge", 17},
		{"high-BW cap at 64", 0, 400 * gib, "g6e.12xlarge", 64},
		{"high-BW no size → 32", 0, 0, "p5.48xlarge", 32},
		{"high-BW tiny model → at least 1", 0, 1 * gib, "p5.48xlarge", 1},
	}
	for _, c := range cases {
		if got := StreamerConcurrency(c.explicit, c.sizeBytes, c.instance); got != c.want {
			t.Errorf("%s: StreamerConcurrency(%d,%d,%q) = %d, want %d", c.name, c.explicit, c.sizeBytes, c.instance, got, c.want)
		}
	}
}
