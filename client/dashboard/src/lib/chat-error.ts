// extractStreamError pulls a human-readable message out of an error surfaced
// by the AI SDK stream. Handles three response shapes:
//   1. OpenRouter:        { error: { message: ..., metadata?: { raw } } }
//   2. Gram (goa):        { name, message, ... } — top-level (e.g. 402 insufficient_credits)
//   3. Plain Error:       error.message
export const extractStreamError = (event: {
  error: unknown;
}): string | undefined => {
  if (typeof event.error !== "object" || event.error === null) {
    return undefined;
  }

  const errorObject = event.error as {
    responseBody?: unknown;
    message?: unknown;
    [key: string]: unknown;
  };

  if (typeof errorObject.responseBody === "string") {
    try {
      const parsedBody = JSON.parse(errorObject.responseBody);
      if (typeof parsedBody !== "object" || parsedBody === null) {
        return undefined;
      }

      if (parsedBody.error) {
        if (parsedBody.error.metadata?.raw) {
          try {
            const rawError = JSON.parse(parsedBody.error.metadata.raw);
            if (rawError.error?.message) {
              return rawError.error.message;
            }
          } catch {
            // fall through to parsedBody.error.message below
          }
        }
        if (typeof parsedBody.error.message === "string") {
          return parsedBody.error.message;
        }
      }

      if (typeof parsedBody.message === "string") {
        return parsedBody.message;
      }
    } catch (e) {
      console.error(`Error parsing model error: ${e}`);
    }
    return undefined;
  }

  if (typeof errorObject.message === "string") {
    return errorObject.message;
  }

  return undefined;
};
