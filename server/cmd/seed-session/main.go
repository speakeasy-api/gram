// seed-session pre-populates a Claude hook session in Redis for testing.
// Usage: go run ./server/cmd/seed-session --session-id=X --project-id=Y --org-id=Z
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

type SessionMetadata struct {
	SessionID   string
	ServiceName string
	UserEmail   string
	ClaudeOrgID string
	GramOrgID   string
	ProjectID   string
}

func main() {
	sessionID := flag.String("session-id", "", "Session ID to seed")
	projectID := flag.String("project-id", "", "Project ID")
	orgID := flag.String("org-id", "", "Organization ID")
	flag.Parse()

	if *sessionID == "" || *projectID == "" || *orgID == "" {
		fmt.Fprintln(os.Stderr, "Usage: seed-session --session-id=X --project-id=Y --org-id=Z")
		os.Exit(1)
	}

	addr := os.Getenv("GRAM_REDIS_CACHE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:5445"
	}
	pass := os.Getenv("GRAM_REDIS_CACHE_PASSWORD")

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: pass,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis ping: %v", err)
	}

	c := cache.NewRedisCacheAdapter(client)
	key := fmt.Sprintf("session:metadata:%s", *sessionID)

	meta := SessionMetadata{
		SessionID:   *sessionID,
		ServiceName: "claude",
		UserEmail:   "test@example.com",
		GramOrgID:   *orgID,
		ProjectID:   *projectID,
	}

	if err := c.Set(ctx, key, meta, 5*time.Minute); err != nil {
		log.Fatalf("set: %v", err)
	}

	fmt.Printf("Seeded session %s -> project %s (org %s)\n", *sessionID, *projectID, *orgID)
}
