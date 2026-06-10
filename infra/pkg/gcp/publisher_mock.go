package gcp

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type mockPublishResult struct{ err error }

func (mpr *mockPublishResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (mpr *mockPublishResult) Get(ctx context.Context) (string, error) {
	return "rand-id", mpr.err
}

type MockPublisher[M any] struct {
	mock mock.Mock
}

func NewMockPublisher[M any]() *MockPublisher[M] {
	return &MockPublisher[M]{}
}

func (m *MockPublisher[M]) Publish(ctx context.Context, msg M) PublishResult {
	args := m.mock.Called(ctx, msg)

	a0 := args.Get(0)
	if a0 == nil {
		return &mockPublishResult{err: nil}
	}

	switch arg0 := a0.(type) {
	case PublishResult:
		return arg0
	case error:
		return &mockPublishResult{err: arg0}
	default:
		panic("unexpected return type from mock: " + args.String(0))
	}
}
