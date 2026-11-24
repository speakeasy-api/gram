import { z } from "zod";
import {
  DASHBOARD_CONFIG_META_NAME,
  DASHBOARD_CONFIG_PLACEHOLDER,
} from "./dashboard-config-html";

// Deployment-specific dashboard settings, supplied at container start so one
// image serves every partition and environment. The deployment sets
// GRAM_DASHBOARD_CONFIG to a JSON object (rendered by the gram-infra Helm
// chart); the image entrypoint (42-dashboard-config.sh) HTML-escapes it into
// the gram-dashboard-config meta tag in index.html, and the browser unescapes
// the attribute back to JSON. Under `vite dev` the dashboard-config plugin in
// vite.config.ts substitutes the same environment variable instead.
//
// Every field is optional. An unset field turns its feature off or falls back
// to a value derived from the current host; see each consumer.

const nonEmptyString = z.string().trim().min(1);

// A bare host with an optional port: it is placed into URLs, so a scheme,
// path or whitespace would build a broken one. Lowercased to match
// URL.hostname.
const host = nonEmptyString
  .regex(/^[a-z0-9.-]+(:\d+)?$/i)
  .transform((value) => value.toLowerCase());

// A bare hostname without a port: compared against URL.hostname, or shown to
// customers as a DNS target.
const hostname = nonEmptyString
  .regex(/^[a-z0-9.-]+$/i)
  .transform((value) => value.toLowerCase());

const httpsURL = nonEmptyString.refine((value) => {
  try {
    return new URL(value).protocol === "https:";
  } catch {
    return false;
  }
});

// An optional setting that drops itself, with an error naming it, when its
// value is invalid, so one bad value disables only that setting.
function setting<T extends z.ZodType>(name: string, schema: T) {
  return schema.optional().catch(() => {
    console.error(
      `Ignoring invalid dashboard runtime configuration setting "${name}".`,
    );
    return undefined;
  });
}

// Unknown keys are stripped rather than rejected, so infra can add a setting
// before the image that reads it ships.
const runtimeConfigSchema = z
  .object({
    posthogKey: setting("posthogKey", nonEmptyString),
    posthogHost: setting("posthogHost", httpsURL),
    datadogRumApplicationId: setting("datadogRumApplicationId", nonEmptyString),
    datadogRumClientToken: setting("datadogRumClientToken", nonEmptyString),
    datadogRumSite: setting("datadogRumSite", hostname),
    datadogRumEnv: setting("datadogRumEnv", nonEmptyString),
    tunnelHost: setting("tunnelHost", host),
    customDomainCname: setting("customDomainCname", hostname),
    pylonAppId: setting("pylonAppId", nonEmptyString),
    firstPartyHosts: setting("firstPartyHosts", z.array(hostname).nonempty()),
  })
  .catch(() => {
    console.error(
      "Ignoring dashboard runtime configuration that is not a JSON object.",
    );
    return {};
  });

export type RuntimeConfig = z.infer<typeof runtimeConfigSchema>;

/**
 * Parses the meta tag content. Anything that is not a JSON object, including
 * the empty image default and an unsubstituted placeholder, yields an empty
 * config.
 */
export function parseRuntimeConfig(
  raw: string | null | undefined,
): RuntimeConfig {
  if (!raw) return {};
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    if (raw !== DASHBOARD_CONFIG_PLACEHOLDER) {
      console.error("Ignoring malformed dashboard runtime configuration.");
    }
    return {};
  }
  return runtimeConfigSchema.parse(value);
}

let cached: { raw: string | null; config: RuntimeConfig } | undefined;

export function getRuntimeConfig(): RuntimeConfig {
  if (typeof document === "undefined") return {};
  const raw =
    document
      .querySelector<HTMLMetaElement>(
        `meta[name="${DASHBOARD_CONFIG_META_NAME}"]`,
      )
      ?.getAttribute("content") ?? null;
  if (cached === undefined || cached.raw !== raw) {
    cached = { raw, config: parseRuntimeConfig(raw) };
  }
  return cached.config;
}

export type PostHogConfig = { key: string; apiHost: string };

/** PostHog settings, or null when this deployment does not use PostHog. */
export function posthogConfig(
  config: RuntimeConfig = getRuntimeConfig(),
): PostHogConfig | null {
  if (!config.posthogKey || !config.posthogHost) return null;
  return { key: config.posthogKey, apiHost: config.posthogHost };
}

/** Whether any Datadog RUM setting is present, complete or not. */
export function hasAnyDatadogRumSetting(
  config: RuntimeConfig = getRuntimeConfig(),
): boolean {
  return [
    config.datadogRumApplicationId,
    config.datadogRumClientToken,
    config.datadogRumSite,
    config.datadogRumEnv,
  ].some((value) => value !== undefined);
}

export type DatadogRumConfig = {
  applicationId: string;
  clientToken: string;
  site: string;
  env: string;
};

/** Datadog RUM settings, or null when RUM is off for this deployment. */
export function datadogRumConfig(
  config: RuntimeConfig = getRuntimeConfig(),
): DatadogRumConfig | null {
  const {
    datadogRumApplicationId: applicationId,
    datadogRumClientToken: clientToken,
    datadogRumSite: site,
    datadogRumEnv: env,
  } = config;
  if (!applicationId || !clientToken || !site || !env) return null;
  return { applicationId, clientToken, site, env };
}
