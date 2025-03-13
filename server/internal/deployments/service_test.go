package deployments_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/dbtest"
)

var (
	cloneTestDatabase dbtest.PostgresDBCloneFunc
)

func TestMain(m *testing.M) {
	container, cloner, err := dbtest.NewPostgres(context.Background())
	if err != nil {
		log.Fatalf("Failed to start postgres container: %v", err)
		os.Exit(1)
	}
	defer container.Terminate(context.Background())

	cloneTestDatabase = cloner

	os.Exit(m.Run())
}

func TestDeploymentsService(t *testing.T) {
	var conn *pgxpool.Pool
	var err error
	for range 5 {
		conn, err = cloneTestDatabase(t, "testdb")
		if err != nil {
			t.Fatalf("Failed to clone test database: %v", err)
		}
		defer conn.Close()
	}

	rows, err := conn.Query(t.Context(), "SELECT datname, datistemplate FROM pg_database WHERE datistemplate = false;")
	if err != nil {
		t.Fatalf("Failed to query databases: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dbName string
		var isTemplate bool
		if err := rows.Scan(&dbName, &isTemplate); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}
		fmt.Printf("Database: %s, Is Template: %v\n", dbName, isTemplate)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("Error iterating rows: %v", err)
	}

	rows, err = conn.Query(t.Context(), `
		SELECT table_name 
		FROM information_schema.tables
		WHERE table_schema = 'public'
		ORDER BY table_name;
	`)
	if err != nil {
		t.Fatalf("Failed to query tables: %v", err)
	}
	defer rows.Close()

	fmt.Println("\nTables:")
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}
		fmt.Printf("- %s\n", tableName)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("Error iterating rows: %v", err)
	}
}
