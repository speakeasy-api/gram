import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { PolicyEditorFor } from "./PolicyDetail";

vi.mock("@/components/page-layout", () => ({
  Page: {
    Section: {
      Title: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
    },
  },
}));
afterEach(cleanup);
it("keeps an unsupported policy readable without exposing an editor", () => {
  const policy = {
    name: "Example guardrail",
    action: unrecognized("future_action"),
    policyType: "standard",
    audienceType: "everyone",
    enabled: true,
    prompt: "Keep confidential content private.",
  } as RiskPolicy;
  render(<PolicyEditorFor policy={policy} />);
  expect(screen.getByRole("heading", { name: policy.name })).toBeTruthy();
  expect(screen.getByText(policy.prompt!)).toBeTruthy();
  expect(screen.getByText("Unknown")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Refresh page" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
  expect(screen.queryByRole("textbox")).toBeNull();
});
