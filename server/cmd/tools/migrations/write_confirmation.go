package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// writeConfirmation is the flag set a PostgreSQL backfill uses to tie a write
// to the database it actually connects to, rather than to a descriptive
// environment label alone.
type writeConfirmation struct {
	name               string
	apply              *bool
	environment        *string
	confirmEnvironment *string
	confirmTarget      *string
	confirmApply       *string
}

// registerWriteConfirmation adds -apply, -environment and the -confirm-*
// flags to fs. name is the value -confirm-apply must equal.
func registerWriteConfirmation(fs *flag.FlagSet, name string) *writeConfirmation {
	return &writeConfirmation{
		name:               name,
		apply:              fs.Bool("apply", false, "write the backfill (default is dry run)"),
		environment:        fs.String("environment", "", "target environment name"),
		confirmEnvironment: fs.String("confirm-environment", "", "must exactly match environment for writes"),
		confirmTarget:      fs.String("confirm-target", "", "must exactly match the parsed host, port, and database for writes"),
		confirmApply:       fs.String("confirm-apply", "", "must equal "+name+" for writes"),
	}
}

// check validates the parsed flags against dbURL and reports whether the run
// writes.
func (c *writeConfirmation) check(dbURL string) (bool, error) {
	if *c.environment == "" {
		return false, errors.New("environment is required")
	}
	if dbURL == "" {
		return false, errors.New("missing $GRAM_DATABASE_URL")
	}
	dbConfig, err := pgx.ParseConfig(dbURL)
	if err != nil {
		return false, errors.New("invalid $GRAM_DATABASE_URL")
	}
	if !*c.apply {
		return false, nil
	}
	if *c.confirmEnvironment != *c.environment {
		return false, errors.New("writes require -confirm-environment to exactly match -environment")
	}
	target := net.JoinHostPort(dbConfig.Host, strconv.Itoa(int(dbConfig.Port))) + "/" + dbConfig.Database
	if *c.confirmTarget != target {
		return false, fmt.Errorf("writes require -confirm-target=%s to match the parsed database target", target)
	}
	if *c.confirmApply != c.name {
		return false, errors.New("writes require -confirm-apply=" + c.name)
	}
	return true, nil
}
