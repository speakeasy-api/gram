package repo

import "context"

type Queries struct{}

func New(db any) *Queries { return &Queries{} }

func (q *Queries) WriteChatMessage(_ context.Context) error { return nil }
