import { z } from "zod";

import { type Context } from "./machine-types";

export type GuardResult = { ok: true } | { ok: false; reason: string };

const ExternalMetadataSchema = z
  .object({
    authorization_endpoint: z.url(),
    token_endpoint: z.url(),
    registration_endpoint: z.url(),
  })
  .loose();

const ExternalMetadataJsonSchema = z
  .string()
  .transform((s, ctx) => {
    let parsed: unknown;
    try {
      parsed = JSON.parse(s);
    } catch {
      ctx.addIssue({ code: "custom", message: "Invalid JSON format" });
      return z.NEVER;
    }
    if (
      typeof parsed !== "object" ||
      parsed === null ||
      Array.isArray(parsed)
    ) {
      ctx.addIssue({
        code: "custom",
        message: "Metadata must be a JSON object",
      });
      return z.NEVER;
    }
    return parsed;
  })
  .pipe(ExternalMetadataSchema);

function formatMetadataError(err: z.ZodError): string {
  // Top-level errors from the JSON-parse transform have an empty path.
  const topLevel = err.issues.find((i) => i.path.length === 0);
  if (topLevel) return topLevel.message;
  // Field-level errors: list the offending keys (missing or malformed URL).
  const fields = err.issues
    .map((i) => i.path[i.path.length - 1])
    .filter((p): p is string => typeof p === "string");
  if (fields.length > 0) {
    return `Invalid or missing endpoints: ${fields.join(", ")}`;
  }
  return "Invalid metadata";
}

export function checkExternal(ctx: Context): GuardResult {
  if (!ctx.external.slug.trim()) {
    return { ok: false, reason: "Please provide a slug for the OAuth server" };
  }
  const result = ExternalMetadataJsonSchema.safeParse(
    ctx.external.metadataJson,
  );
  if (result.success) return { ok: true };
  return { ok: false, reason: formatMetadataError(result.error) };
}

export function checkProxyMeta(ctx: Context): GuardResult {
  if (!ctx.proxy.slug.trim()) {
    return {
      ok: false,
      reason: "Please provide a slug for the OAuth proxy server",
    };
  }
  if (!ctx.proxy.authorizationEndpoint.trim()) {
    return { ok: false, reason: "Authorization endpoint is required" };
  }
  if (!ctx.proxy.tokenEndpoint.trim()) {
    return { ok: false, reason: "Token endpoint is required" };
  }
  return { ok: true };
}

export function checkCreds(ctx: Context): GuardResult {
  if (!ctx.proxy.clientId.trim() || !ctx.proxy.clientSecret.trim()) {
    return {
      ok: false,
      reason: "Client ID and Client Secret are required",
    };
  }
  return { ok: true };
}

export const validExternal = (ctx: Context): boolean => checkExternal(ctx).ok;
export const validProxyMeta = (ctx: Context): boolean => checkProxyMeta(ctx).ok;
export const validCreds = (ctx: Context): boolean => checkCreds(ctx).ok;
