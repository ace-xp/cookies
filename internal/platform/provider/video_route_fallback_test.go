package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// missingVideoRoutes stands in for a deployment whose Settings page has never
// been filled in: MySQL holds no enabled video route.
type missingVideoRoutes struct{}

func (missingVideoRoutes) ResolveVideoRoute(context.Context, contract.OrganizationID, string) (VideoRouteSnapshot, error) {
	return VideoRouteSnapshot{}, ErrGatewayRouteNotFound
}

func videoFallbackRequest() CreateVideoJobRequest {
	brandID := contract.BrandID("brand_1")
	return CreateVideoJobRequest{
		Actor: contract.ActorContext{
			OrganizationID: "org_1",
			Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: "user_1"},
			Scopes:         []contract.Scope{ScopeJobCreate},
		},
		Project: contract.ProjectContext{
			OrganizationID:        "org_1",
			ProjectID:             "project_1",
			BrandID:               &brandID,
			ProductIDs:            []contract.ProductID{},
			ProjectContextVersion: 7,
		},
		IdempotencyKey: "create-video-fallback-1",
		RequestHash:    strings.Repeat("d", 64),
		ModelAlias:     VideoModelAlias,
		SourceSystem:   "creative",
		SourceTaskID:   "creative_task_1",
		Input: VideoGenerationInput{
			Prompt:          "A five second original vertical matcha pre-roll",
			DurationSeconds: 5,
			AspectRatio:     "9:16",
			Resolution:      "720p",
		},
	}
}

func newVideoFallbackService(routeOptional bool) (Service, *memoryStore, *recordingScheduler) {
	now := time.Date(2026, time.August, 15, 8, 0, 0, 0, time.UTC)
	store := &memoryStore{}
	scheduler := &recordingScheduler{}
	return Service{
		Store:              store,
		Scheduler:          scheduler,
		VideoRoutes:        missingVideoRoutes{},
		VideoRouteOptional: routeOptional,
		NewID:              func() (string, error) { return "provider_video_job_1", nil },
		Now:                func() time.Time { return now },
	}, store, scheduler
}

func TestCreateVideoJobFallsBackToAdapterCredentialWhenRouteIsOptional(t *testing.T) {
	t.Parallel()
	service, store, scheduler := newVideoFallbackService(true)
	job, duplicate, err := service.CreateVideoJob(context.Background(), videoFallbackRequest())
	if err != nil || duplicate {
		t.Fatalf("CreateVideoJob() duplicate=%v err=%v", duplicate, err)
	}
	if job.ExecutionStatus != contract.JobQueued || scheduler.calls != 1 {
		t.Fatalf("expected a queued and scheduled job: job=%+v scheduler=%+v", job, scheduler)
	}
	if len(store.records) != 1 || store.records[0].Route != nil {
		t.Fatalf("expected the stored record to carry no route: %+v", store.records)
	}
}

func TestCreateVideoJobFailsWhenRouteIsRequiredButMissing(t *testing.T) {
	t.Parallel()
	service, _, scheduler := newVideoFallbackService(false)
	if _, _, err := service.CreateVideoJob(context.Background(), videoFallbackRequest()); err == nil {
		t.Fatal("expected a missing video route to fail job creation")
	}
	if scheduler.calls != 0 {
		t.Fatalf("expected no execution to be scheduled: %+v", scheduler)
	}
}
