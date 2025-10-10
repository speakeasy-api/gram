package customdomains

import (
	"context"

	"github.com/google/uuid"
)

type ctxKey int

const (
	domainKey ctxKey = iota
)

type Context struct {
	OrganizationID string
	Domain         string
	DomainID       uuid.UUID
}

func WithContext(ctx context.Context, value *Context) context.Context {
	return context.WithValue(ctx, domainKey, value)
}

func FromContext(ctx context.Context) *Context {
	val := ctx.Value(domainKey)
	if val == nil {
		return nil
	}

	domain, ok := val.(*Context)
	if !ok || domain == nil {
		return nil
	}

	return domain
}
