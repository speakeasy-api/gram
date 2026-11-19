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

// A bare hostname without a port, compared against URL.hostname.
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

// Each field catches its own failure, so one invalid value drops only that
// setting. Unknown keys are stripped rather than rejected, so infra can add a
// setting before the image that reads it ships.
const runtimeConfigSchema = z
  .object({
    posthogKey: nonEmptyString.optional().catch(undefined),
    posthogHost: httpsURL.optional().catch(undefined),
    datadogRumApplicationId: nonEmptyString.optional().catch(undefined),
    datadogRumClientToken: nonEmptyString.optional().catch(undefined),
    datadogRumSite: host.optional().catch(undefined),
    datadogRumEnv: nonEmptyString.optional().catch(undefined),
    tunnelHost: host.optional().catch(undefined),
    customDomainCname: host.optional().catch(undefined),
    pylonAppId: nonEmptyString.optional().catch(undefined),
    firstPartyHosts: z.array(hostname).nonempty().optional().catch(undefined),
  })
  .catch({});

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
