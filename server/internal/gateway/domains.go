package gateway

import (
	"context"

	"github.com/google/uuid"
)

type ctxKey int

const (
	domainKey ctxKey = iota
)

type DomainContext struct {
	OrganizationID string
	Domain         string
	DomainID       uuid.UUID
}

func DomainWithContext(ctx context.Context, value *DomainContext) context.Context {
	return context.WithValue(ctx, domainKey, value)
}

func DomainFromContext(ctx context.Context) *DomainContext {
	val := ctx.Value(domainKey)
	if val == nil {
		return nil
	}

	domain, ok := val.(*DomainContext)
	if !ok || domain == nil {
		return nil
	}

	return domain
}
