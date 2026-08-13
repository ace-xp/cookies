package assets

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

type derivativeRepoStub struct {
	value AssetDerivative
	job   ProcessingJob
}

func (r *derivativeRepoStub) EnsureDerivative(_ context.Context, derivative AssetDerivative, job ProcessingJob) (AssetDerivative, ProcessingJob, bool, error) {
	if r.value.ID != "" {
		return r.value, r.job, true, nil
	}
	r.value, r.job = derivative, job
	return derivative, job, false, nil
}
func (r *derivativeRepoStub) RetryDerivative(_ context.Context, org contract.OrganizationID, project contract.ProjectID, id, newJobID string, now time.Time) (AssetDerivative, ProcessingJob, error) {
	if r.value.OrganizationID != org || r.value.ProjectID != project || r.value.ID != id || r.job.Status != ProcessingFailed {
		return AssetDerivative{}, ProcessingJob{}, ErrInvalidState
	}
	r.value.Status, r.value.ErrorCode, r.value.UpdatedAt = DerivativeQueued, "", now
	r.job = ProcessingJob{ID: newJobID, OrganizationID: org, ProjectID: project, DerivativeID: id, Status: ProcessingQueued, Attempt: r.job.Attempt + 1, CreatedAt: now, UpdatedAt: now}
	return r.value, r.job, nil
}
func (r *derivativeRepoStub) FailDerivativeScheduling(_ context.Context, id, code string, now time.Time) (AssetDerivative, ProcessingJob, error) {
	r.value.Status, r.value.ErrorCode, r.value.UpdatedAt = DerivativeFailed, code, now
	r.job.Status, r.job.ErrorCode, r.job.UpdatedAt = ProcessingFailed, code, now
	return r.value, r.job, nil
}
func (r *derivativeRepoStub) GetDerivative(_ context.Context, org contract.OrganizationID, project contract.ProjectID, source contract.AssetVersionRef, profile DerivativeProfile) (AssetDerivative, error) {
	if r.value.ID == "" || r.value.OrganizationID != org || r.value.ProjectID != project ||
		r.value.Source != source || r.value.Profile != profile {
		return AssetDerivative{}, ErrNotFound
	}
	return r.value, nil
}
func (r *derivativeRepoStub) GetDerivativeByID(_ context.Context, id string) (AssetDerivative, error) {
	if r.value.ID != id {
		return AssetDerivative{}, ErrNotFound
	}
	return r.value, nil
}
func (r *derivativeRepoStub) CompleteDerivative(_ context.Context, id string, output contract.AssetVersionRef, now time.Time) (AssetDerivative, error) {
	if r.value.ID != id {
		return AssetDerivative{}, ErrNotFound
	}
	r.value.Status, r.value.Output, r.value.ErrorCode, r.value.UpdatedAt = DerivativeReady, &output, "", now
	return r.value, nil
}

type derivativeSchedulerStub struct {
	calls int
	err   error
}

func (s *derivativeSchedulerStub) ScheduleAssetDerivative(context.Context, ProcessingJob) error {
	s.calls++
	return s.err
}

func TestEnsureDerivativeDeduplicatesAnAssetVersionAndProfile(t *testing.T) {
	repo, scheduler := &derivativeRepoStub{}, &derivativeSchedulerStub{}
	sequence := 0
	service := DerivativeService{Repository: repo, Scheduler: scheduler, NewID: func(prefix string) (string, error) { sequence++; return prefix + "_" + string(rune('0'+sequence)), nil }}
	request := EnsureDerivativeRequest{OrganizationID: "org_1", ProjectID: "project_1", AssetRef: contract.AssetVersionRef{AssetID: "asset_1", Version: 2}, Profile: DerivativeVideoProxy}
	first, _, duplicate, err := service.EnsureDerivative(context.Background(), request)
	if err != nil || duplicate {
		t.Fatalf("first ensure: %v duplicate=%v", err, duplicate)
	}
	second, _, duplicate, err := service.EnsureDerivative(context.Background(), request)
	if err != nil || !duplicate || first.ID != second.ID || scheduler.calls != 1 {
		t.Fatalf("dedupe failed: %#v %#v calls=%d err=%v", first, second, scheduler.calls, err)
	}
}

func TestDerivativeSchedulingFailureIsTerminalAndRetryable(t *testing.T) {
	repo, scheduler := &derivativeRepoStub{}, &derivativeSchedulerStub{err: errors.New("queue unavailable")}
	service := DerivativeService{Repository: repo, Scheduler: scheduler, NewID: func(prefix string) (string, error) { return prefix + "_1", nil }}
	request := EnsureDerivativeRequest{OrganizationID: "org_1", ProjectID: "project_1", AssetRef: contract.AssetVersionRef{AssetID: "asset_1", Version: 1}, Profile: DerivativePoster}
	value, job, _, err := service.EnsureDerivative(context.Background(), request)
	if err == nil || value.Status != DerivativeFailed || job.Status != ProcessingFailed {
		t.Fatalf("queue failure must terminate records: %#v %#v %v", value, job, err)
	}
	scheduler.err = nil
	value, job, err = service.RetryDerivative(context.Background(), "org_1", "project_1", value.ID)
	if err != nil || value.Status != DerivativeQueued || job.Status != ProcessingQueued || scheduler.calls != 2 {
		t.Fatalf("retry failed: %#v %#v %v", value, job, err)
	}
}

func TestFindDerivativeRequiresConfiguredRepository(t *testing.T) {
	service := DerivativeService{}
	_, err := service.FindDerivative(context.Background(), "k_org_1", "k_project_1",
		contract.AssetVersionRef{AssetID: "asset_1", Version: 1}, DerivativePoster)
	if err == nil {
		t.Fatal("没有仓储时应当报错")
	}
}

func TestFindDerivativeRejectsUnknownProfile(t *testing.T) {
	service := DerivativeService{Repository: &derivativeRepoStub{}}
	_, err := service.FindDerivative(context.Background(), "k_org_1", "k_project_1",
		contract.AssetVersionRef{AssetID: "asset_1", Version: 1}, DerivativeProfile("thumbnail_v9"))
	if err == nil {
		t.Fatal("不认识的派生物规格应当被拒")
	}
}
