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
it.each([
  { action: unrecognized("future_action"), sources: ["gitleaks"] },
  { action: "block", sources: ["future_detector"] },
  { action: "block", sources: ["gitleaks", "future_detector"] },
])(
  "keeps an unsupported policy readable without exposing an editor: %j",
  (settings) => {
    const policy = {
      name: "Example guardrail",
      ...settings,
      policyType: "standard",
      audienceType: "everyone",
      enabled: true,
      prompt: "Keep confidential content private.",
    } as RiskPolicy;
    render(<PolicyEditorFor policy={policy} />);
    expect(screen.getByRole("heading", { name: policy.name })).toBeTruthy();
    expect(screen.getByText(policy.prompt!)).toBeTruthy();
    expect(screen.getByText(/cannot safely edit/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Refresh page" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /save/i })).toBeNull();
    expect(screen.queryByRole("textbox")).toBeNull();
  },
);
