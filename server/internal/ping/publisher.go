package ping

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	pingv1 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func StartPublisher(ctx context.Context, logger *slog.Logger, broker gcp.PublisherBroker) error {
	pub, err := gcp.PubSubPublisherForMessage(ctx, broker, &pingv1.Message{})
	if err != nil {
		return fmt.Errorf("get publisher for ping messages: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Second):
		}

		id, err := uuid.NewV7()
		if err != nil {
			logger.ErrorContext(ctx, "failed to generate UUID for ping message", attr.SlogError(err))
			continue
		}

		msg := pingv1.Message_builder{
			Id:        new(id.String()),
			Type:      new("simulated"),
			CreatedAt: timestamppb.Now(),
			Payload:   []byte(`{"msg":"Hello, World!"}`),
		}.Build()

		_, err = pub.Publish(ctx, msg).Get(ctx)
		switch {
		case errors.Is(err, context.Canceled):
			return nil
		case err != nil:
			return fmt.Errorf("publish ping message: %w", err)
		}
	}
}
