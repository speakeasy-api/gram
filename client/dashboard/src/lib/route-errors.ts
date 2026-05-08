import { GramError } from "@gram/client/models/errors/gramerror.js";

export function getHttpStatusCode(error: unknown): number | undefined {
  if (error instanceof GramError) {
    return error.statusCode;
  }

  if (error && typeof error === "object") {
    const statusCode = (error as { statusCode?: unknown }).statusCode;
    if (typeof statusCode === "number") {
      return statusCode;
    }

    const status = (error as { status?: unknown }).status;
    if (typeof status === "number") {
      return status;
    }
  }

  return undefined;
}

export function isNotFoundError(error: unknown): boolean {
  return getHttpStatusCode(error) === 404;
}

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isUuidRouteParam(value: string | undefined): value is string {
  return typeof value === "string" && UUID_PATTERN.test(value);
}
