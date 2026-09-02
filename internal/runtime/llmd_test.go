package runtime

import (
	"strings"
	"testing"
)

func TestLLMD_Registered(t *testing.T) {
	rt, err := Get("llm-d")
	if err != nil {
		t.Fatalf("llm-d not registered: %v", err)
	}
	if !IsMultiNode(rt) {
		t.Error("llm-d should report IsMultiNode() == true")
	}
	if got := rt.ContainerName(); got != "vllm" {
		t.Errorf("ContainerName = %q, want vllm", got)
	}
	if accels := rt.SupportedAccelerators(); len(accels) != 1 || accels[0] != "gpu" {
		t.Errorf("SupportedAccelerators = %v, want [gpu]", accels)
	}
}

// TestLLMD_ImageAndVersion covers PRD-66 Part 2: the llm-d-aws image tag comes
// from ToolVersions.LLMDVersion (its own release line), NOT FrameworkVersion
// (which for an llm-d run is the bundled vLLM engine version). DefaultImage
// composes the repo + configured tag; an empty version falls back to the pin.
func TestLLMD_ImageAndVersion(t *testing.T) {
	rt := &LLMD{}
	// ResolveVersion reads LLMDVersion, not FrameworkVersion.
	tv := ToolVersions{FrameworkVersion: "v0.19.0", LLMDVersion: "v0.9.0"}
	if got := rt.ResolveVersion(tv); got != "v0.9.0" {
		t.Errorf("ResolveVersion = %q, want v0.9.0 (LLMDVersion, not FrameworkVersion)", got)
	}
	// DefaultImage composes repo + tag.
	if got := rt.DefaultImage("v0.9.0", ""); got != "ghcr.io/llm-d/llm-d-aws:v0.9.0" {
		t.Errorf("DefaultImage(v0.9.0) = %q", got)
	}
	// Empty version → the shipped default pin.
	if got := rt.DefaultImage("", ""); got != "ghcr.io/llm-d/llm-d-aws:"+DefaultLLMDVersion {
		t.Errorf("DefaultImage(\"\") = %q, want default pin", got)
	}
	// PRD-66 Part 2a: with a pull-through registry, the image routes through the
	// GHCR ECR pull-through cache (ghcr prefix maps to ghcr.io).
	if got := rt.DefaultImage("v0.9.0", "123.dkr.ecr.us-east-2.amazonaws.com"); got != "123.dkr.ecr.us-east-2.amazonaws.com/ghcr/llm-d/llm-d-aws:v0.9.0" {
		t.Errorf("DefaultImage with pull-through = %q", got)
	}
	// LLMDImage helper: direct (no pull-through) and cached forms.
	if got := LLMDImage("v1.2.3", ""); got != "ghcr.io/llm-d/llm-d-aws:v1.2.3" {
		t.Errorf("LLMDImage direct = %q", got)
	}
	if got := LLMDImage("v1.2.3", "123.dkr.ecr.us-east-2.amazonaws.com"); got != "123.dkr.ecr.us-east-2.amazonaws.com/ghcr/llm-d/llm-d-aws:v1.2.3" {
		t.Errorf("LLMDImage pull-through = %q", got)
	}
	// Empty version + pull-through → default pin, still cached.
	if got := LLMDImage("", "123.dkr.ecr.us-east-2.amazonaws.com"); got != "123.dkr.ecr.us-east-2.amazonaws.com/ghcr/llm-d/llm-d-aws:"+DefaultLLMDVersion {
		t.Errorf("LLMDImage empty+pull-through = %q", got)
	}
}

func TestSingleNodeRuntimes_NotMultiNode(t *testing.T) {
	for _, name := range []string{"vllm", "vllm-neuron", "sglang"} {
		rt, err := Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		if IsMultiNode(rt) {
			t.Errorf("%q must NOT be multi-node", name)
		}
	}
}

func TestLLMD_BuildArgs_EmitsTPAndPP(t *testing.T) {
	rt := &LLMD{}
	command, args := rt.BuildArgs(ContainerParams{
		ModelHfID:              "meta-llama/Llama-3.1-70B",
		TensorParallelDegree:   8,
		PipelineParallelDegree: 2,
		NodeCount:              2,
		GPUsPerNode:            8,
	})
	// BuildArgs returns only model + static tuning flags; command is nil (the
	// deployment template wraps everything in /bin/bash -c and appends the
	// multi-node coordination flags from LWS env).
	if command != nil {
		t.Errorf("command should be nil (template supplies it), got %v", command)
	}
	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "meta-llama/Llama-3.1-70B ") {
		t.Errorf("model should be the first (positional) arg; got: %s", joined)
	}
	if !strings.Contains(joined, "--trust-remote-code") {
		t.Errorf("args missing --trust-remote-code; got: %s", joined)
	}
	// TP/PP and DP flags are NOT emitted by BuildArgs — they come from the
	// template (they depend on LWS runtime env).
	for _, notWant := range []string{"--tensor-parallel-size", "--pipeline-parallel-size", "--data-parallel", "--model "} {
		if strings.Contains(joined, notWant) {
			t.Errorf("BuildArgs should NOT emit %q (template does); got: %s", notWant, joined)
		}
	}
}

func TestLLMD_BuildArgs_ModelIsPositional(t *testing.T) {
	rt := &LLMD{}
	_, args := rt.BuildArgs(ContainerParams{ModelHfID: "m"})
	if len(args) == 0 || args[0] != "m" {
		t.Errorf("model should be the leading positional arg; got: %v", args)
	}
}

func TestLLMD_BuildArgs_S3Streamer(t *testing.T) {
	rt := &LLMD{}
	_, args := rt.BuildArgs(ContainerParams{
		ModelS3URI:           "s3://bucket/model",
		UseRunaiStreamer:     true,
		TensorParallelDegree: 4,
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--load-format runai_streamer") {
		t.Errorf("expected runai_streamer load-format; got: %s", joined)
	}
	if !strings.Contains(joined, "s3://bucket/model") {
		t.Errorf("expected S3 URI as model; got: %s", joined)
	}
	// TP=4 (>1) → distributed:true; no explicit/memory-limit → default S3
	// concurrency 32 (run.ai benchmark optimum).
	if !strings.Contains(joined, `{"concurrency":32,"distributed":true}`) {
		t.Errorf("expected concurrency+distributed extra-config for TP>1; got: %s", joined)
	}
}

// TestLLMD_BuildArgs_StreamerMemoryLimit (PRD-65 Layer 3): a configured
// StreamerMemoryLimitGiB is emitted as memory_limit (bytes) in the
// --model-loader-extra-config JSON.
func TestLLMD_BuildArgs_StreamerMemoryLimit(t *testing.T) {
	rt := &LLMD{}
	_, args := rt.BuildArgs(ContainerParams{
		ModelS3URI:             "s3://bucket/model",
		UseRunaiStreamer:       true,
		StreamerConcurrency:    8,
		StreamerMemoryLimitGiB: 16,
	})
	joined := strings.Join(args, " ")
	// 16 GiB = 17179869184 bytes; concurrency honored.
	if !strings.Contains(joined, `{"concurrency":8,"memory_limit":17179869184}`) {
		t.Errorf("expected concurrency+memory_limit extra-config; got: %s", joined)
	}
}

// TestStreamerExtraConfig covers the shared JSON builder directly. Concurrency is
// bandwidth-aware: on a standard instance it's the S3-benchmarked default (32) or
// an explicit override; on a high-bandwidth instance (>=50 Gbps) with a known
// model size it's AWS's ceil(size_gb/4gb). distributed only for TP>1; memory_limit
// when set.
func TestStreamerExtraConfig(t *testing.T) {
	gib := int64(1024 * 1024 * 1024)
	cases := []struct {
		name string
		p    ContainerParams
		want string
	}{
		{"standard default (TP=1)", ContainerParams{InstanceTypeName: "g6.2xlarge"}, `{"concurrency":32}`},
		{"explicit concurrency wins", ContainerParams{StreamerConcurrency: 4, InstanceTypeName: "g6.2xlarge"}, `{"concurrency":4}`},
		{"TP>1 adds distributed", ContainerParams{TensorParallelDegree: 2, InstanceTypeName: "g6.2xlarge"}, `{"concurrency":32,"distributed":true}`},
		{"TP=1 no distributed", ContainerParams{TensorParallelDegree: 1, InstanceTypeName: "g6.2xlarge"}, `{"concurrency":32}`},
		{"standard ignores size (no AWS formula on low-BW)", ContainerParams{ModelSizeBytes: 67 * gib, InstanceTypeName: "g6.2xlarge"}, `{"concurrency":32}`},
		{"high-BW size-derived (67GiB → ceil(67/4)=17)", ContainerParams{ModelSizeBytes: 67 * gib, InstanceTypeName: "p5.48xlarge"}, `{"concurrency":17}`},
		{"high-BW size cap at 64 (400GiB)", ContainerParams{ModelSizeBytes: 400 * gib, InstanceTypeName: "g6e.12xlarge"}, `{"concurrency":64}`},
		{"high-BW explicit beats size", ContainerParams{StreamerConcurrency: 8, ModelSizeBytes: 67 * gib, InstanceTypeName: "p5.48xlarge"}, `{"concurrency":8}`},
		{"high-BW no size → default 32", ContainerParams{InstanceTypeName: "p5.48xlarge"}, `{"concurrency":32}`},
		{"high-BW all: size + TP>1 + memlimit", ContainerParams{ModelSizeBytes: 10 * gib, TensorParallelDegree: 2, StreamerMemoryLimitGiB: 1, InstanceTypeName: "p5.48xlarge"},
			`{"concurrency":3,"distributed":true,"memory_limit":1073741824}`},
	}
	for _, c := range cases {
		if got := streamerExtraConfig(c.p); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
