package memory

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/memory"
)

// fakeMemoryService captures inputs and returns scripted results across all
// three Service methods so a single shared double covers all platform memory
// tool tests.
type fakeMemoryService struct {
	rememberCalls int
	recallCalls   int
	forgetCalls   int

	gotAssist  uuid.UUID
	gotProject uuid.UUID
	gotOrg     string
	gotContent string
	gotQuery   string
	gotLimit   int
	gotTags    []string

	rememberResult memory.RememberResult
	rememberErr    error
	recallResult   []memory.RecallResult
	recallErr      error
	forgetResult   memory.ForgetResult
	forgetErr      error
}

var _ Service = (*fakeMemoryService)(nil)

func (f *fakeMemoryService) Remember(
	_ context.Context,
	assistantID uuid.UUID,
	projectID uuid.UUID,
	organizationID string,
	content string,
	tags []string,
) (memory.RememberResult, error) {
	f.rememberCalls++
	f.gotAssist = assistantID
	f.gotProject = projectID
	f.gotOrg = organizationID
	f.gotContent = content
	f.gotTags = tags
	return f.rememberResult, f.rememberErr
}

func (f *fakeMemoryService) Recall(
	_ context.Context,
	assistantID uuid.UUID,
	organizationID string,
	query string,
	limit int,
	tags []string,
) ([]memory.RecallResult, error) {
	f.recallCalls++
	f.gotAssist = assistantID
	f.gotOrg = organizationID
	f.gotQuery = query
	f.gotLimit = limit
	f.gotTags = tags
	return f.recallResult, f.recallErr
}

func (f *fakeMemoryService) Forget(
	_ context.Context,
	assistantID uuid.UUID,
	projectID uuid.UUID,
	organizationID string,
	query string,
	tags []string,
) (memory.ForgetResult, error) {
	f.forgetCalls++
	f.gotAssist = assistantID
	f.gotProject = projectID
	f.gotOrg = organizationID
	f.gotQuery = query
	f.gotTags = tags
	return f.forgetResult, f.forgetErr
}
