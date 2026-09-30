package tunnelmetrics

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{ClickHouse: true})
	if err != nil {
		log.Fatal(err)
	}
	infra = res
	code := m.Run()
	if err = cleanup(); err != nil {
		log.Print(err)
		code = 1
	}
	os.Exit(code)
}
