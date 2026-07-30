import type { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";
import { z } from "zod";

export type MetadataValidationResult =
  | { ok: true; metadata: Record<string, unknown> }
  | { ok: false; reason: string };

const AbsoluteHttpUrlSchema = z
  .url()
  .refine(
    (value) => value.startsWith("https://") || value.startsWith("http://"),
  );

const ExternalMetadataSchema = z
  .object({
    issuer: AbsoluteHttpUrlSchema,
    authorization_endpoint: AbsoluteHttpUrlSchema,
    token_endpoint: AbsoluteHttpUrlSchema,
    registration_endpoint: AbsoluteHttpUrlSchema,
  })
  .loose();

const ExternalMetadataJsonSchema = z
  .string()
  .transform((value, context) => {
    let parsed: unknown;
    try {
      parsed = JSON.parse(value);
    } catch {
      context.addIssue({ code: "custom", message: "Invalid JSON format" });
      return z.NEVER;
    }

    if (
      typeof parsed !== "object" ||
      parsed === null ||
      Array.isArray(parsed)
    ) {
      context.addIssue({
        code: "custom",
        message: "Metadata must be a JSON object",
      });
      return z.NEVER;
    }

    return parsed;
  })
  .pipe(ExternalMetadataSchema);

function formatMetadataError(error: z.ZodError): string {
  const topLevel = error.issues.find((issue) => issue.path.length === 0);
  if (topLevel) return topLevel.message;

  const fields = error.issues
    .map((issue) => issue.path[issue.path.length - 1])
    .filter((field): field is string => typeof field === "string");

  if (fields.length > 0) {
    return `Invalid or missing OAuth metadata: ${fields.join(", ")}`;
  }

  return "Invalid OAuth metadata";
}

export function validateIssuerUrl(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return "Issuer URL is required";
  if (!AbsoluteHttpUrlSchema.safeParse(trimmed).success) {
    return "Enter an absolute HTTP or HTTPS issuer URL";
  }
  return null;
}

export function validateExternalMetadataJson(
  value: string,
  expectedIssuer?: string,
): MetadataValidationResult {
  const result = ExternalMetadataJsonSchema.safeParse(value);
  if (!result.success) {
    return { ok: false, reason: formatMetadataError(result.error) };
  }

  if (
    expectedIssuer &&
    result.data.issuer.replace(/\/$/, "") !==
      expectedIssuer.trim().replace(/\/$/, "")
  ) {
    return {
      ok: false,
      reason: "Metadata issuer must match the Issuer URL",
    };
  }

  return { ok: true, metadata: result.data };
}

export function metadataFromIssuerDraft(
  draft: RemoteSessionIssuerDraft,
): Record<string, unknown> {
  const metadata: Record<string, unknown> = {
    issuer: draft.issuer,
  };

  const optionalStrings: Array<[string, string | undefined]> = [
    ["authorization_endpoint", draft.authorizationEndpoint],
    ["token_endpoint", draft.tokenEndpoint],
    ["registration_endpoint", draft.registrationEndpoint],
    ["jwks_uri", draft.jwksUri],
    ["service_documentation", draft.serviceDocumentation],
    ["op_policy_uri", draft.opPolicyUri],
    ["op_tos_uri", draft.opTosUri],
  ];
  for (const [key, value] of optionalStrings) {
    if (value) metadata[key] = value;
  }

  const optionalArrays: Array<[string, string[] | undefined]> = [
    ["scopes_supported", draft.scopesSupported],
    ["grant_types_supported", draft.grantTypesSupported],
    ["response_types_supported", draft.responseTypesSupported],
    [
      "token_endpoint_auth_methods_supported",
      draft.tokenEndpointAuthMethodsSupported,
    ],
  ];
  for (const [key, value] of optionalArrays) {
    if (value?.length) metadata[key] = value;
  }

  return metadata;
}
