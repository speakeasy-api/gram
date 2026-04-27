// dev-api-key creates or reuses a dev API key for local hook testing.
// Prints the plaintext key to stdout. Meant for scripting.
//
// Usage: go run ./server/cmd/dev-api-key --project-slug=ecommerce-api
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	projectSlug := flag.String("project-slug", "", "Project slug to create the key for")
	flag.Parse()

	if *projectSlug == "" {
		fmt.Fprintln(os.Stderr, "Usage: dev-api-key --project-slug=<slug>")
		os.Exit(1)
	}

	dbURL := os.Getenv("GRAM_DATABASE_URL")
	if dbURL == "" {
		log.Fatal("GRAM_DATABASE_URL not set")
	}

	ctx := context.Background()
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	// Look up project + org
	var projectID uuid.UUID
	var orgID string
	err = db.QueryRow(ctx,
		"SELECT id, organization_id FROM projects WHERE slug = $1 AND deleted IS FALSE LIMIT 1",
		*projectSlug,
	).Scan(&projectID, &orgID)
	if err != nil {
		log.Fatalf("project %q not found: %v", *projectSlug, err)
	}

	// Check for existing dev key
	var existingPrefix string
	err = db.QueryRow(ctx,
		"SELECT key_prefix FROM api_keys WHERE project_id = $1 AND name = 'dev-hooks-test' AND deleted IS FALSE LIMIT 1",
		projectID,
	).Scan(&existingPrefix)
	if err == nil {
		// Key exists but we can't recover the plaintext (it's hashed).
		// Delete and recreate.
		_, _ = db.Exec(ctx,
			"UPDATE api_keys SET deleted_at = NOW() WHERE project_id = $1 AND name = 'dev-hooks-test' AND deleted IS FALSE",
			projectID,
		)
	}

	// Generate a new key
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		log.Fatalf("generate token: %v", err)
	}
	tokenHex := hex.EncodeToString(token)
	fullKey := "gram_local_" + tokenHex
	prefix := "gram_local_" + tokenHex[:5]

	hash := sha256.Sum256([]byte(fullKey))
	keyHash := hex.EncodeToString(hash[:])

	// Find a user to attribute the key to
	var userID string
	err = db.QueryRow(ctx, "SELECT id FROM users LIMIT 1").Scan(&userID)
	if err != nil {
		log.Fatalf("no users found: %v", err)
	}

	// Insert the key
	_, err = db.Exec(ctx,
		`INSERT INTO api_keys (organization_id, project_id, created_by_user_id, name, key_prefix, key_hash, scopes)
		 VALUES ($1, $2, $3, 'dev-hooks-test', $4, $5, '{hooks}')`,
		orgID, projectID, userID, prefix, keyHash,
	)
	if err != nil {
		log.Fatalf("insert api key: %v", err)
	}

	// Print the plaintext key (only output, for scripting)
	fmt.Print(fullKey)
}
