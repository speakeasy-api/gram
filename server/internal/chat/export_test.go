package chat

import "context"

// Test seams exposing the (unexported) tool-activity summarizer to the
// black-box chat_test package. The request types' fields are already exported;
// these aliases just make the types nameable from outside the package.

type SummarizeToolActivityRequest = summarizeToolActivityRequest

type ToolActivityCallInput = toolActivityCallInput

// SummarizeToolActivityForTest calls the raw handler's core logic directly,
// bypassing HTTP/auth wiring so tests can drive it with a seeded auth context.
func (s *Service) SummarizeToolActivityForTest(ctx context.Context, req *SummarizeToolActivityRequest) (string, error) {
	return s.summarizeToolActivity(ctx, req)
}
