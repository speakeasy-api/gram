import { afterEach, describe, expect, it } from "vitest";
import {
  rememberLandingUtm,
  setAnalyticsSessionIdSource,
  signupAttributionParams,
} from "./signup-attribution";

afterEach(() => {
  window.sessionStorage.clear();
  setAnalyticsSessionIdSource(null);
});

describe("signupAttributionParams", () => {
  it("forwards the sign-up page's own UTMs and the analytics session", () => {
    setAnalyticsSessionIdSource(() => "sess-123");
    expect(
      signupAttributionParams(
        "?utm_source=linkedin&utm_campaign=q4&partner_code=x",
      ),
    ).toEqual({
      utm_source: "linkedin",
      utm_campaign: "q4",
      analytics_session_id: "sess-123",
    });
  });

  it("falls back to the UTMs remembered at boot", () => {
    rememberLandingUtm("?utm_source=google&utm_medium=cpc");
    expect(signupAttributionParams("")).toEqual({
      utm_source: "google",
      utm_medium: "cpc",
    });
  });

  it("prefers the current URL over remembered UTMs, without merging", () => {
    rememberLandingUtm("?utm_source=google&utm_medium=cpc");
    expect(signupAttributionParams("?utm_source=linkedin")).toEqual({
      utm_source: "linkedin",
    });
  });

  it("returns nothing when there is nothing to attribute", () => {
    rememberLandingUtm("?redirect=/x");
    expect(signupAttributionParams("")).toEqual({});
  });
});
