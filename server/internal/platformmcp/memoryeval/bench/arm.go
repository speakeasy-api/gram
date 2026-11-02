// Package bench runs the team memory eval against a corpus that is already
// loaded into a database. The judged test, and later the platform-admin eval
// runs, share it: arms expose memory tools to an answering agent, Run asks
// every question and grades the answers with a judge model, and Report holds
// the per model and per arm scores.
package bench

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
)

// Target is a corpus loaded into one organization.
type Target struct {
	DB             *pgxpool.Pool
	Corpus         *memoryeval.Corpus
	OrganizationID string

	// Projects maps corpus projects to their stored rows.
	Projects map[memoryeval.ProjectKey]Project

	// UserID maps a corpus user handle to its stored user ID.
	UserID func(handle string) string
}

// Project is the stored identity of one corpus project.
type Project struct {
	ID   uuid.UUID
	Slug string
}

// ArmResult is what one arm search or sweep returned.
type ArmResult struct {
	// Output is the exact text the tools returned, as an agent would see it.
	Output string

	// ChatIDs are the chats the returned items came from.
	ChatIDs []uuid.UUID
}

// Tool is one tool an arm offers the answering agent.
type Tool struct {
	Name        string
	Description string

	// Schema is the JSON Schema of the tool's arguments.
	Schema string
}

// Arm is one way of giving an agent memory. Search runs a keyword query over
// the main project's full history; Sweep probes what the arm shows for the
// main project, for leak and isolation checks. An arm with a browse mode
// sweeps everything; an arm without one (search_team_memory is search only)
// probes with every term the corpus plants: evidence, titles, canaries and
// secrets. So a sweep is exhaustive for planted content, not for arbitrary
// text a summary might invent. Tools, Call and
// Instructions are what the judged eval gives the answering agent. Every
// function carries its own principal and grants, so callers pass a plain
// context.
type Arm struct {
	Name   string
	Search func(ctx context.Context, query string) (ArmResult, error)
	Sweep  func(ctx context.Context) (ArmResult, error)

	Tools        []Tool
	Call         func(ctx context.Context, tool, arguments string) (string, error)
	Instructions string
}

// NoMemoryArm is arm A: the agent answers without tools.
func NoMemoryArm() Arm {
	return Arm{Name: "A: no memory", Search: nil, Sweep: nil, Tools: nil, Call: nil, Instructions: ""}
}
