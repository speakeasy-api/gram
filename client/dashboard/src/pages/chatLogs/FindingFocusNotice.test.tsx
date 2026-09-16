import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FindingFocusNotice } from "./FindingFocusNotice";

function finding(extra: Partial<RiskResult> = {}): RiskResult {
  return {
    id: "finding-1",
    chatMessageId: "message-1",
    policyId: "policy-1",
    policyVersion: 1,
    createdAt: new Date("2026-09-16T00:00:00Z"),
    source: "gitleaks",
    ruleId: "aws-access-token",
    match: "AKIAEXAMPLE",
    ...extra,
  };
}

afterEach(cleanup);

describe("FindingFocusNotice", () => {
  it("says the value is highlighted once the finding is located", () => {
    render(
      <FindingFocusNotice
        finding={finding()}
        isLoading={false}
        located
        onJump={vi.fn<() => void>()}
      />,
    );

    expect(screen.getByText("Opened from evidence")).toBeTruthy();
    expect(screen.getByText("aws-access-token")).toBeTruthy();
    expect(
      screen.getByText(
        "The matched value is highlighted in the message below.",
      ),
    ).toBeTruthy();
  });

  it("promises no highlighted value for a finding that marks no span", () => {
    // A judge finding's match is the whole event it read, so there is no span
    // in the message to point at.
    render(
      <FindingFocusNotice
        finding={finding({
          source: "prompt_injection",
          ruleId: "prompt_injection",
          match: '{"body":"ignore your instructions"}',
        })}
        isLoading={false}
        located
        onJump={vi.fn<() => void>()}
      />,
    );

    expect(
      screen.getByText("The message it flagged is highlighted below."),
    ).toBeTruthy();
  });

  it("explains the missing scope when the matched value was withheld", () => {
    render(
      <FindingFocusNotice
        finding={finding({
          match: undefined,
          matchRedacted: "<redacted len=20 sha=abcd12>",
        })}
        isLoading={false}
        located
        onJump={vi.fn<() => void>()}
      />,
    );

    // Landing in the transcript with no highlight is the reported confusion:
    // the notice has to name the scope that would reveal the value.
    expect(screen.getByText(/chat:read/)).toBeTruthy();
    expect(screen.getByText(/stays masked/)).toBeTruthy();
  });

  it("offers a re-scroll once the message is loaded", () => {
    const onJump = vi.fn<() => void>();
    render(
      <FindingFocusNotice
        finding={finding()}
        isLoading={false}
        located
        onJump={onJump}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Jump to message" }));
    expect(onJump).toHaveBeenCalledTimes(1);
  });

  it("points at loading the rest of the session when the message isn't loaded", () => {
    render(
      <FindingFocusNotice
        finding={finding()}
        isLoading={false}
        located={false}
        onJump={vi.fn<() => void>()}
      />,
    );

    expect(screen.getByText(/load all messages/)).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Jump to message" }),
    ).toBeNull();
  });

  it("says the finding is gone rather than silently rendering nothing", () => {
    render(
      <FindingFocusNotice
        finding={undefined}
        isLoading={false}
        located={false}
        onJump={vi.fn<() => void>()}
      />,
    );

    expect(screen.getByText(/no longer open for this session/)).toBeTruthy();
  });

  it("shows a locating state while the session's findings load", () => {
    render(
      <FindingFocusNotice
        finding={undefined}
        isLoading
        located={false}
        onJump={vi.fn<() => void>()}
      />,
    );

    expect(screen.getByText("Locating the flagged message…")).toBeTruthy();
  });
});
