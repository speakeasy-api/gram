// Package classifiertest provides test doubles for classifier consumers.
package classifiertest

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/speakeasy-api/gram/server/internal/classifier"
)

// Mock implements classifier.Classifier using testify expectations. Configure
// Classify with
// Return(classifier.Result). The context and request are both recorded.
type Mock struct {
	// Mock provides testify's expectation and call assertion methods.
	mock.Mock
}

var _ classifier.Classifier = (*Mock)(nil)

// NewMock registers expectation verification with the test's cleanup lifecycle.
func NewMock(t interface {
	mock.TestingT
	Cleanup(func())
}) *Mock {
	var m Mock
	m.Test(t)
	t.Cleanup(func() { m.AssertExpectations(t) })
	return &m
}

// Classify records the call and returns the configured result.
func (m *Mock) Classify(ctx context.Context, request *classifier.Request) classifier.Result {
	args := m.Called(ctx, request)
	result, ok := args.Get(0).(classifier.Result)
	if !ok {
		panic("classifiertest.Mock.Classify: first return value must be classifier.Result")
	}
	return result
}
