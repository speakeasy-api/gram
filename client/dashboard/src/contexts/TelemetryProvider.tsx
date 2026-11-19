import {
  rememberLandingUtm,
  setAnalyticsSessionIdSource,
} from "@/lib/signup-attribution";
import { datadogRumConfig, posthogConfig } from "@/lib/runtime-config";
import { getServerURL } from "@/lib/utils";
import posthog from "posthog-js";
import { type ReactNode, useEffect } from "react";
import { datadogRum } from "@datadog/browser-rum";
import {
  TelemetryStateProvider,
  nullTelemetry,
  testTelemetry,
  devTelemetry,
} from "./Telemetry";
import type { Telemetry } from "./Telemetry";

// Set this to true to test telemetry locally
const AM_TESTING_TELEMETRY = false;

// The public skill share page (/shared/skills/:token) carries a secret
// capability token in its URL and in its getShared XHR. Never initialize
// telemetry there: PostHog pageview capture and Datadog RUM view/resource
// tracking would otherwise upload the token to external analytics backends.
// A load-time check is sufficient because the page is standalone — the
// dashboard never SPA-navigates to or from /shared/*.
const isPublicSharePath = (): boolean =>
  window.location.pathname.startsWith("/shared/");

// Localhost skips PostHog flag evaluation so every flag is on. Preview
// hosts keep talking to the real project so configured flags can be
// toggled; only missing keys fail open (see failOpenMissingFlags).
export const shouldUseDevTelemetry = (serverURL: string): boolean =>
  serverURL.includes("localhost");

// PR preview hosts (*.dev.getgram.ai). Shared staging is the apex host
// (dev.getgram.ai) and must not match.
export const shouldFailOpenMissingFlags = (serverURL: string): boolean =>
  serverURL.includes(".dev.getgram.ai");

export function failOpenMissingFlags(telemetry: Telemetry): Telemetry {
  return {
    isFeatureEnabled: ((flag, options) => {
      const result = telemetry.isFeatureEnabled(flag, options);
      return result === undefined ? true : result;
    }) as Telemetry["isFeatureEnabled"],
    // Variants pass through untouched: a missing multivariate key stays
    // undefined so callers fall back to the boolean read above rather than
    // inventing a variant.
    getFeatureFlag: telemetry.getFeatureFlag.bind(telemetry),
    onFeatureFlags: telemetry.onFeatureFlags.bind(telemetry),
    capture: telemetry.capture.bind(telemetry),
    identify: telemetry.identify.bind(telemetry),
    register: telemetry.register.bind(telemetry),
    reset: telemetry.reset.bind(telemetry),
    group: telemetry.group.bind(telemetry),
  };
}

// Speakeasy's own hosts: getgram.ai, ai.speakeasy.com and their subdomains. Matched
// on label boundaries so lookalikes such as mygetgram.ai do not qualify.
export function isGramHost(serverURL: string): boolean {
  const host = new URL(serverURL).hostname;
  return ["getgram.ai", "ai.speakeasy.com"].some(
    (domain) => host === domain || host.endsWith(`.${domain}`),
  );
}

// Before the router can redirect away from the landing URL.
rememberLandingUtm();

let warnedMissingPostHog = false;

export const TelemetryProvider = (props: {
  children: ReactNode;
}): JSX.Element => {
  const serverURL = getServerURL();
  // PostHog and Datadog RUM settings come from the deployment (see
  // lib/runtime-config.ts). A deployment that sets none runs without either.
  const posthogSettings = posthogConfig();
  if (
    !posthogSettings &&
    !warnedMissingPostHog &&
    !shouldUseDevTelemetry(serverURL)
  ) {
    // Without PostHog every feature flag reads false. Every deployment except
    // local development is expected to configure it (GRAM_DASHBOARD_CONFIG).
    warnedMissingPostHog = true;
    console.warn(
      "Dashboard runtime configuration has no PostHog settings; feature flags are off.",
    );
  }
  const ph =
    isPublicSharePath() || !posthogSettings
      ? null
      : posthog.init(
          posthogSettings.key,
          {
            api_host: posthogSettings.apiHost,
            // PostHog otherwise collects the hardware model by default on
            // Chromium-based Android browsers.
            disableDeviceModel: true,
            // Session recording is otherwise switched on remotely from the
            // PostHog project settings. Keep it off regardless of that setting.
            disable_session_recording: true,
            feature_flag_request_timeout_ms: 1000,
          },
          "speakeasy",
        );
  setAnalyticsSessionIdSource(ph ? () => ph.get_session_id() : null);

  useEffect(() => {
    // Never start RUM on the public share page — the resource entries would
    // capture the share token in URLs.
    if (isPublicSharePath()) {
      return;
    }

    // Guard against duplicate initialization (can happen in React StrictMode or component remounts)
    if (datadogRum.getInitConfiguration()) {
      return;
    }

    const rum = datadogRumConfig();
    if (rum) {
      datadogRum.init({
        applicationId: rum.applicationId,
        clientToken: rum.clientToken,
        site: rum.site,
        service: "gram",
        env: rum.env,
        sessionSampleRate: 100,
        // No session replay: recordings would show customer data on screen.
        sessionReplaySampleRate: 0,
        trackUserInteractions: true,
        trackResources: true,
        defaultPrivacyLevel: "mask-user-input",
        version: __GRAM_GIT_SHA__ || null,
      });
    }
  }, []);

  let value: Telemetry = ph ?? nullTelemetry;
  // Share page uses nullTelemetry and never emits onFeatureFlags.
  let flagsInitiallyAvailable = ph == null;
  if (!isPublicSharePath() && shouldUseDevTelemetry(serverURL)) {
    value = AM_TESTING_TELEMETRY ? testTelemetry : devTelemetry;
    flagsInitiallyAvailable = true;
  } else if (
    !isPublicSharePath() &&
    ph &&
    shouldFailOpenMissingFlags(serverURL)
  ) {
    value = failOpenMissingFlags(ph);
  }

  return (
    <TelemetryStateProvider
      telemetry={value}
      featureFlagsInitiallyAvailable={flagsInitiallyAvailable}
    >
      {props.children}
    </TelemetryStateProvider>
  );
};
