package testenv

import (
	"context"
	"log"
	"os"
	"testing"
)

var infra *Environment

func TestMain(m *testing.M) {
	res, cleanup, err := Launch(context.Background(), LaunchOptions{Temporal: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("clean up test infrastructure: %v", err)
	}

	os.Exit(code)
}
