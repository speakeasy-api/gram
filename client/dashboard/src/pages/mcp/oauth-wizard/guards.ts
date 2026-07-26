import { type Context } from "./machine-types";
import {
  validateExternalMetadataJson,
  validateIssuerUrl,
} from "./externalOAuthMetadata";

export type GuardResult = { ok: true } | { ok: false; reason: string };

export function checkExternal(ctx: Context): GuardResult {
  if (!ctx.external.slug.trim()) {
    return { ok: false, reason: "Please provide a slug for the OAuth server" };
  }
  const issuerError = validateIssuerUrl(ctx.external.issuerUrl);
  if (issuerError) return { ok: false, reason: issuerError };

  const result = validateExternalMetadataJson(
    ctx.external.metadataJson,
    ctx.external.issuerUrl,
  );
  if (result.ok) return { ok: true };
  return result;
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
