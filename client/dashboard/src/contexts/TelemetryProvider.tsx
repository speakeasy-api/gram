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

// Localhost and PR preview hosts (*.dev.getgram.ai) skip PostHog flag
// evaluation so gated UI such as Watchdog and Functions is visible. Shared
// staging (dev.getgram.ai) and production stay on the real project.
export const shouldUseDevTelemetry = (serverURL: string): boolean =>
  serverURL.includes("localhost") || serverURL.includes(".dev.getgram.ai");

export const TelemetryProvider = (props: {
  children: ReactNode;
}): JSX.Element => {
  const serverURL = getServerURL();
  const isProd = serverURL.includes("app.getgram.ai");
  const ph = isPublicSharePath()
    ? null
    : posthog.init(
        isProd
          ? "phc_hiYSF5Axu49I1xs4Z5BG8KCI3PGNLM8ERRs7eocmfX9"
          : "phc_5S3YhOs1lONwM2yKa0ytVSAyWOR2GhVhwAebkyi022l",
        {
          api_host: "https://metrics.speakeasy.com",
          // PostHog otherwise collects the hardware model by default on
          // Chromium-based Android browsers.
          disableDeviceModel: true,
          feature_flag_request_timeout_ms: 1000,
        },
        "speakeasy",
      );

  useEffect(() => {
    // Never start RUM on the public share page — the session replay and
    // resource entries would capture the share token in URLs.
    if (isPublicSharePath()) {
      return;
    }

    // Guard against duplicate initialization (can happen in React StrictMode or component remounts)
    if (datadogRum.getInitConfiguration()) {
      return;
    }

    const serverURL = getServerURL();
    if (serverURL.includes("getgram.ai")) {
      const env = serverURL.includes("app.getgram.ai") ? "prod" : "dev";
      datadogRum.init({
        applicationId: "93afb64a-dd15-490c-a749-51b4c5c5a171",
        clientToken: "pub8358667232c624e2f91e1eaa0bd380fd",
        site: "datadoghq.com",
        service: "gram",
        env,
        sessionSampleRate: 100,
        sessionReplaySampleRate: 100,
        trackUserInteractions: true,
        trackResources: true,
        defaultPrivacyLevel: "mask-user-input",
        version: __GRAM_GIT_SHA__ || null,
      });
    }
  }, []);

  let value: Telemetry = ph ?? nullTelemetry;
  if (!isPublicSharePath() && shouldUseDevTelemetry(getServerURL())) {
    value = AM_TESTING_TELEMETRY ? testTelemetry : devTelemetry;
  }

  return (
    <TelemetryStateProvider
      telemetry={value}
      featureFlagsInitiallyAvailable={value !== ph}
    >
      {props.children}
    </TelemetryStateProvider>
  );
};
