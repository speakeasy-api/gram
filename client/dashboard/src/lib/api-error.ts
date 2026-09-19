import { getHttpStatusCode } from "@/lib/route-errors";

export type ApiErrorKind =
  | "bad_request"
  | "precondition"
  | "rate_limited"
  | "forbidden"
  | "unavailable"
  | "other";

export type ApiErrorDescription = {
  kind: ApiErrorKind;
  status?: number;
  title: string;
  message: string;
};

const TITLES: Record<ApiErrorKind, string> = {
  bad_request: "Check the value",
  precondition: "Cannot continue",
  rate_limited: "Rate limited",
  forbidden: "Not allowed",
  unavailable: "Speakeasy could not complete this right now",
  other: "Something went wrong",
};

const FALLBACK_MESSAGES: Record<ApiErrorKind, string> = {
  bad_request: "Speakeasy could not accept that value.",
  precondition: "This is not possible in the current state.",
  rate_limited: "Too many attempts. Wait a minute and try again.",
  forbidden: "You need the org:admin role to do this.",
  unavailable: "The upstream service could not be reached. Try again shortly.",
  other: "The request failed. Try again.",
};

const KIND_BY_STATUS: Record<number, ApiErrorKind> = {
  400: "bad_request",
  409: "precondition",
  412: "precondition",
  429: "rate_limited",
  403: "forbidden",
  502: "unavailable",
  503: "unavailable",
  504: "unavailable",
};

function kindForStatus(status: number | undefined): ApiErrorKind {
  if (status === undefined) return "other";
  return KIND_BY_STATUS[status] ?? "other";
}

function readMessage(error: unknown): string | undefined {
  if (typeof error !== "object" || error === null) return undefined;
  const message = (error as { message?: unknown }).message;
  if (typeof message !== "string") return undefined;
  const trimmed = message.trim();
  if (trimmed === "" || trimmed.startsWith("API error occurred")) {
    return undefined;
  }
  return trimmed;
}

/** Turn an SDK error into inline copy; the API's own message wins when it has one. */
export function describeApiError(error: unknown): ApiErrorDescription {
  const status = getHttpStatusCode(error);
  const kind = kindForStatus(status);
  return {
    kind,
    status,
    title: TITLES[kind],
    message: readMessage(error) ?? FALLBACK_MESSAGES[kind],
  };
}

export function apiErrorAlertVariant(
  kind: ApiErrorKind,
): "error" | "warning" | "info" {
  switch (kind) {
    case "rate_limited":
    case "precondition":
      return "warning";
    case "unavailable":
      return "info";
    case "bad_request":
    case "forbidden":
    case "other":
      return "error";
  }
}
