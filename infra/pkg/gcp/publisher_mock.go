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

type successPublishResult struct{}

func NewSuccessPublishResult() PublishResult {
	return &successPublishResult{}
}

func (spr *successPublishResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (spr *successPublishResult) Get(ctx context.Context) (string, error) {
	return "rand-id", nil
}

type MockPublisher[M any] struct {
	mock.Mock
}

func NewMockPublisher[M any]() *MockPublisher[M] {
	return &MockPublisher[M]{}
}

func (m *MockPublisher[M]) Publish(ctx context.Context, msg M) PublishResult {
	args := m.Called(ctx, msg)

	a0 := args.Get(0)
	switch arg0 := a0.(type) {
	case PublishResult:
		return arg0
	case error:
		return &mockPublishResult{err: arg0}
	default:
		panic("unexpected return type from mock: " + args.String(0))
	}
}

func (m *MockPublisher[M]) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
