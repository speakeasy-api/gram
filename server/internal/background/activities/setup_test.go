package activities_test

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, ClickHouse: true, Redis: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

// captureLoopsClient records every transactional email the activity attempts
// to send so tests can assert on the exact payloads.
type captureLoopsClient struct {
	mu   sync.Mutex
	sent []loops.SendTransactionalInput
	// failNext makes the next SendTransactional calls fail without recording,
	// simulating a transport outage.
	failNext int
}

func (c *captureLoopsClient) SendTransactional(_ context.Context, input loops.SendTransactionalInput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNext > 0 {
		c.failNext--
		return errors.New("loops unavailable")
	}
	c.sent = append(c.sent, input)
	return nil
}

func (c *captureLoopsClient) Sent() []loops.SendTransactionalInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]loops.SendTransactionalInput, len(c.sent))
	copy(out, c.sent)
	return out
}

func (c *captureLoopsClient) FailNext(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failNext = n
}
