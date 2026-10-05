import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { UserIdentityDraft } from "../drafts/useIdentityDraft";
import { GuidedOAuthSetup } from "./GuidedOAuthSetup";
import { getOAuthSetupGuide } from "./registry";
import * as policies from "./oauthPolicies";
import { SlackSetup } from "./SlackSetup";
import type { OAuthSetupGuideProps } from "./types";

vi.mock("./SlackSetup", () => ({
  SlackSetup: vi.fn(({ children }: OAuthSetupGuideProps) => (
    <div data-testid="slack-guide">{children}</div>
  )),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

const draft = {} as UserIdentityDraft;
const fallbackUrls = [
  undefined,
  "",
  "not a URL",
  "https://example.com/mcp",
  "https://mcp.slack.com.evil.example/mcp",
  "https://mcp.slack.com@evil.example/mcp",
  "https://user:password@mcp.slack.com/mcp",
  "http://mcp.slack.com/mcp",
  "https://mcp.slack.com:8443/mcp",
  "https://mcp.slack.com/other",
  "https://mcp.slack.com/mcp?tenant=example",
  "https://mcp.slack.com/mcp#setup",
];

describe("guided OAuth setup registry", () => {
  it.each(policies.oauthSetupPolicies)(
    "registers a guide for policy $id",
    (policy) => {
      vi.spyOn(policies, "getOAuthSetupPolicy").mockReturnValue(policy);
      expect(getOAuthSetupGuide(undefined)).toBeDefined();
    },
  );
  it("selects the Slack guide using the shared reviewed policy", () => {
    expect(getOAuthSetupGuide("https://mcp.slack.com/mcp")).toBe(SlackSetup);
  });

  it("passes the draft, URL, disabled state and fallback into the guide", () => {
    render(
      <GuidedOAuthSetup
        serverUrl="https://mcp.slack.com/mcp"
        draft={draft}
        disabled
      >
        <span>Manual setup</span>
      </GuidedOAuthSetup>,
    );
    expect(screen.getByTestId("slack-guide")).toBeTruthy();
    expect(screen.getByText("Manual setup")).toBeTruthy();
    expect(vi.mocked(SlackSetup).mock.calls[0]?.[0]).toMatchObject({
      serverUrl: "https://mcp.slack.com/mcp",
      draft,
      disabled: true,
    });
  });

  it("falls back when a policy has no registered guide", () => {
    vi.spyOn(policies, "getOAuthSetupPolicy").mockReturnValue({
      ...policies.slackOAuthSetupPolicy,
      id: "unregistered",
    });
    render(
      <GuidedOAuthSetup
        serverUrl="https://mcp.slack.com/mcp"
        draft={draft}
        disabled={false}
      >
        <span>Manual setup</span>
      </GuidedOAuthSetup>,
    );
    expect(screen.getByText("Manual setup")).toBeTruthy();
    expect(SlackSetup).not.toHaveBeenCalled();
  });

  it.each(fallbackUrls)("keeps ordinary setup for %s", (serverUrl) => {
    expect(getOAuthSetupGuide(serverUrl)).toBeUndefined();
    render(
      <GuidedOAuthSetup serverUrl={serverUrl} draft={draft} disabled={false}>
        <span>Manual setup</span>
      </GuidedOAuthSetup>,
    );
    expect(screen.getByText("Manual setup")).toBeTruthy();
    expect(screen.queryByTestId("slack-guide")).toBeNull();
    expect(SlackSetup).not.toHaveBeenCalled();
  });
});
