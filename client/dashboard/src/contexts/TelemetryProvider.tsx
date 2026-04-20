import { getServerURL } from "@/lib/utils";
import posthog from "posthog-js";
import { type ReactNode, useEffect } from "react";
import { datadogRum } from "@datadog/browser-rum";
import {
  TelemetryContext,
  nullTelemetry,
  testTelemetry,
  devTelemetry,
} from "./Telemetry";
import type { Telemetry } from "./Telemetry";

// Set this to true to test telemetry locally
const AM_TESTING_TELEMETRY = false;

export const TelemetryProvider = (props: { children: ReactNode }) => {
  const ph = posthog.init(
    "phc_hiYSF5Axu49I1xs4Z5BG8KCI3PGNLM8ERRs7eocmfX9",
    {
      api_host: "https://metrics.speakeasy.com",
      feature_flag_request_timeout_ms: 1000,
    },
    "speakeasy",
  );

  useEffect(() => {
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
  if (getServerURL().includes("localhost")) {
    value = AM_TESTING_TELEMETRY ? testTelemetry : devTelemetry;
  }

  return (
    <TelemetryContext.Provider value={value}>
      {props.children}
    </TelemetryContext.Provider>
  );
};
