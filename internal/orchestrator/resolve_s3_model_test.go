package orchestrator

import (
	"context"
	"testing"

	"github.com/accelbench/accelbench/internal/database"

	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// TestResolveS3Model covers the shared cached-model policy (PRD-65 Layer 2):
// explicit URI wins; else a cached HF model auto-detects to its S3 URI + Run:ai;
// else HF download with no streamer.
func TestResolveS3Model(t *testing.T) {
	strptr := func(s string) *string { return &s }

	t.Run("explicit S3 URI wins", func(t *testing.T) {
		o := New(k8sfake.NewSimpleClientset(), database.NewMockRepo(), "pod")
		cfg := RunConfig{RunID: "run12345", Request: &database.RunRequest{
			ModelHfID:  "org/model",
			ModelS3URI: "s3://bucket/explicit",
		}}
		sm := o.resolveS3Model(context.Background(), cfg)
		if sm.URI != "s3://bucket/explicit" || !sm.UseRunai {
			t.Errorf("explicit URI: got %+v, want {s3://bucket/explicit true}", sm)
		}
	})

	t.Run("cached HF model auto-detects", func(t *testing.T) {
		repo := database.NewMockRepo()
		_, _ = repo.CreateModelCache(context.Background(), &database.ModelCache{
			HfID: strptr("org/model"), HfRevision: "main",
			S3URI: "s3://bucket/cached", Status: "cached",
		})
		o := New(k8sfake.NewSimpleClientset(), repo, "pod")
		cfg := RunConfig{RunID: "run12345", Request: &database.RunRequest{
			ModelHfID: "org/model", // no explicit URI, no revision → defaults to "main"
		}}
		sm := o.resolveS3Model(context.Background(), cfg)
		if sm.URI != "s3://bucket/cached" || !sm.UseRunai {
			t.Errorf("cached: got %+v, want URI=s3://bucket/cached useRunai=true", sm)
		}
	})

	t.Run("uncached model falls back to HF", func(t *testing.T) {
		o := New(k8sfake.NewSimpleClientset(), database.NewMockRepo(), "pod")
		cfg := RunConfig{RunID: "run12345", Request: &database.RunRequest{
			ModelHfID: "org/not-cached",
		}}
		sm := o.resolveS3Model(context.Background(), cfg)
		if sm.URI != "" || sm.UseRunai {
			t.Errorf("uncached: got %+v, want {\"\" false}", sm)
		}
	})

	t.Run("cache entry not yet cached (still downloading) → HF", func(t *testing.T) {
		repo := database.NewMockRepo()
		_, _ = repo.CreateModelCache(context.Background(), &database.ModelCache{
			HfID: strptr("org/model"), HfRevision: "main",
			S3URI: "s3://bucket/partial", Status: "downloading",
		})
		o := New(k8sfake.NewSimpleClientset(), repo, "pod")
		cfg := RunConfig{RunID: "run12345", Request: &database.RunRequest{
			ModelHfID: "org/model",
		}}
		sm := o.resolveS3Model(context.Background(), cfg)
		if sm.URI != "" || sm.UseRunai {
			t.Errorf("non-cached status: got %+v, want {\"\" false}", sm)
		}
	})
}
