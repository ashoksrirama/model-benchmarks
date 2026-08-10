package runtime

import "strings"

// highBandwidthGbpsThreshold is the sustained-network cutoff (Gbps) at or above
// which we apply AWS's high-bandwidth Run:ai streamer profile (4 GiB chunk +
// size-derived concurrency). Below it, big chunks + low thread counts don't fill
// the pipe, so we use Run:ai's small-chunk / higher-thread default profile.
const highBandwidthGbpsThreshold = 50

// instanceSustainedGbps maps an EC2 GPU instance type to its SUSTAINED network
// bandwidth in Gbps. For burstable instances (the smaller sizes, listed by AWS
// as "baseline / burst", e.g. g6.2xlarge = 5.0 / 10.0) we record the BASELINE —
// the streamer must fill S3 for tens of seconds, well past any burst-credit
// window, so baseline is the honest sustained figure.
//
// Source: AWS EC2 accelerated-computing instance specs, "Network specifications"
// (docs.aws.amazon.com/ec2/latest/instancetypes/ac.html). Keep this in sync with
// the seeded catalog (db/migrations/002_seed_instance_types.sql). An instance not
// listed here is treated as standard-bandwidth (the safe direction: never apply
// large chunks to a pipe we can't confirm is wide).
var instanceSustainedGbps = map[string]int{
	// G5 (A10G)
	"g5.xlarge": 2, "g5.2xlarge": 5, "g5.4xlarge": 10, "g5.8xlarge": 25,
	"g5.12xlarge": 40, "g5.16xlarge": 25, "g5.24xlarge": 50, "g5.48xlarge": 100,
	// G6 (L4)
	"g6.xlarge": 2, "g6.2xlarge": 5, "g6.4xlarge": 10, "g6.8xlarge": 25,
	"g6.12xlarge": 40, "g6.16xlarge": 25, "g6.24xlarge": 50, "g6.48xlarge": 100,
	// G6e (L40S)
	"g6e.xlarge": 2, "g6e.2xlarge": 5, "g6e.4xlarge": 20, "g6e.8xlarge": 25,
	"g6e.12xlarge": 100, "g6e.16xlarge": 35, "g6e.24xlarge": 200, "g6e.48xlarge": 400,
	// G7e
	"g7e.2xlarge": 50, "g7e.4xlarge": 50, "g7e.8xlarge": 100, "g7e.12xlarge": 400,
	"g7e.24xlarge": 800, "g7e.48xlarge": 1600,
	// Gr6 (L4, memory-optimized)
	"gr6.4xlarge": 10, "gr6.8xlarge": 25,
	// P4 / P5 (all high-bandwidth, EFA)
	"p4d.24xlarge": 400, "p4de.24xlarge": 400,
	"p5.48xlarge": 3200, "p5e.48xlarge": 3200, "p5en.48xlarge": 3200,
}

// IsHighBandwidthInstance reports whether an instance's sustained network
// bandwidth is high enough (>= highBandwidthGbpsThreshold) to benefit from AWS's
// large-chunk streamer profile. Unknown instances are treated as standard.
func IsHighBandwidthInstance(instanceName string) bool {
	// Tolerate an accidental leading/trailing space and case; keys are lower.
	name := strings.ToLower(strings.TrimSpace(instanceName))
	return instanceSustainedGbps[name] >= highBandwidthGbpsThreshold
}
