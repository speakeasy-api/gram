package catalogseed

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	env, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatal(err)
	}
	infra = env
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatal(err)
	}
	os.Exit(code)
}
