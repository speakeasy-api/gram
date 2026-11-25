import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { ContextFacts } from "./AttemptDetailSheet";
import type { AttemptDetail } from "./types";

const failedAttempt: AttemptDetail = {
  id: "ref_1",
  time: "2026-11-20T12:00:00Z",
  firstStartedAt: "2026-11-20T12:00:00Z",
  startedAt: "2026-11-20T12:00:00Z",
  count: 1,
  outcome: "failed",
  step: "sign_in",
  reason: "invalid_grant",
  app: { family: "", name: "Unlisted app" },
  steps: [],
  context: { retries: 0 },
};

afterEach(cleanup);

// A failed attempt never signed in, so the fact names where it tried.
it("names the MCP sign-in of an attempt that did not sign in", () => {
  render(<ContextFacts attempt={failedAttempt} providerName="default" />);

  const label = screen.getByText("MCP sign-in");
  expect(label.tagName).toBe("DT");
  expect(label.nextElementSibling?.textContent).toBe("default");
  expect(screen.queryByText("Signed in through")).toBeNull();
});
