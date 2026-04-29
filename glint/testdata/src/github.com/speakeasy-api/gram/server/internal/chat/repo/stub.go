package repo

import "context"

type CreateChatMessageParams struct{}

type Queries struct{}

func New(db any) *Queries { return &Queries{} }

func (q *Queries) CreateChatMessage(_ context.Context, _ []CreateChatMessageParams) (int64, error) {
	return 0, nil
}
