package noclienterrorlogerror

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

// The analyzer under test is configured with the "CodeUnauthorized" and
// "CodeNotFound" codes opted in.
func examples(ctx context.Context, logger *slog.Logger) {
	// Opted-in client-fault codes logged at error level are flagged.
	_ = oops.E(oops.CodeUnauthorized, nil, "x").LogError(ctx, logger) // want `oops error "CodeUnauthorized" is logged at error level`
	_ = oops.E(oops.CodeNotFound, nil, "y").LogError(ctx, logger)     // want `oops error "CodeNotFound" is logged at error level`
	_ = oops.C(oops.CodeUnauthorized).LogError(ctx, logger)           // want `oops error "CodeUnauthorized" is logged at error level`

	// Client-fault code that is not opted in: no diagnostic.
	_ = oops.E(oops.CodeForbidden, nil, "z").LogError(ctx, logger)

	// Already demoted off LogError: no diagnostic.
	_ = oops.E(oops.CodeUnauthorized, nil, "x").LogWarn(ctx, logger)
	_ = oops.E(oops.CodeNotFound, nil, "y").LogInfo(ctx, logger)

	// Server-fault / 5xx code: out of scope.
	_ = oops.E(oops.CodeUnexpected, nil, "boom").LogError(ctx, logger)

	// Non-constant code cannot be resolved here, so it is skipped.
	code := oops.CodeUnauthorized
	_ = oops.E(code, nil, "x").LogError(ctx, logger)

	// Error stored before logging: the receiver is not a direct constructor call.
	e := oops.E(oops.CodeUnauthorized, nil, "x")
	_ = e.LogError(ctx, logger)
}
