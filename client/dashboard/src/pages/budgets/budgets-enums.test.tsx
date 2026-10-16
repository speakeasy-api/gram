import { unrecognized } from "@gram/client/types/unrecognized.js";
import {
  cleanup,
  render,
  renderHook,
  screen,
  act,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { RuleDetailSheet } from "./RuleDetailSheet";
import { RuleSheet } from "./RuleSheet";
import { defaultRuleDraft, type SpendRule } from "./budgets-data";
import { usePreviewBudgetRule } from "./budgets-queries";

const mocks = vi.hoisted(() => ({ mutate: vi.fn() }));
vi.mock("@gram/client/react-query/spendRulesPreviewRule.js", () => ({
  useSpendRulesPreviewRuleMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/spendRulesListEvents.js", () => ({
  useSpendRulesListEvents: () => ({ data: { events: [] }, refetch: vi.fn() }),
}));
vi.mock("@gram/client/react-query/spendRulesActorAttributes.js", () => ({
  useSpendRulesActorAttributes: () => ({ data: { attributes: [] } }),
}));

afterEach(cleanup);
beforeEach(() => mocks.mutate.mockClear());

function rule(overrides: Partial<SpendRule> = {}): SpendRule {
  return {
    ...defaultRuleDraft(),
    name: "Team budget",
    target: {
      attribute: "department_name",
      operator: "equals",
      value: "Engineering",
    },
    id: "rule-id",
    organizationId: "org-id",
    slug: "team-budget",
    urn: "spend_rule:team-budget:v1",
    version: 1,
    targetExpr: "",
    ruleExpr: "",
    createdAt: new Date("2026-10-01"),
    updatedAt: new Date("2026-10-01"),
    ...overrides,
  };
}

it("skips unsupported preview windows without throwing, then previews known windows", () => {
  const { result } = renderHook(() => usePreviewBudgetRule());
  act(() =>
    result.current.preview(rule({ windowKind: unrecognized("future_window") })),
  );
  expect(mocks.mutate).not.toHaveBeenCalled();
  act(() => result.current.preview(rule()));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        previewSpendRuleRequestBody: expect.objectContaining({
          windowKind: "monthly",
        }),
      },
    }),
    expect.anything(),
  );
});

it("renders an unsupported stored window without a reset countdown or empty-match claim", () => {
  render(
    <RuleDetailSheet
      rule={rule({ windowKind: unrecognized("future_window") })}
      usage={undefined}
      onClose={vi.fn<() => void>()}
      onEdit={vi.fn<(rule: SpendRule) => void>()}
    />,
  );
  expect(screen.getByText(/Unknown window/)).toBeTruthy();
  expect(screen.queryByText(/resets in/)).toBeNull();
  expect(
    screen.getByText(/Usage preview is unavailable for this window/),
  ).toBeTruthy();
  expect(screen.queryByText(/No members match/)).toBeNull();
  expect(mocks.mutate).not.toHaveBeenCalled();
});

it.each([
  { windowKind: unrecognized("future_window") },
  { action: unrecognized("future_action") },
])("explains why unsupported rules cannot be saved: %j", (unsupported) => {
  render(
    <RuleSheet
      open
      onOpenChange={vi.fn<(open: boolean) => void>()}
      rule={rule(unsupported)}
      onSubmit={vi.fn<() => void>()}
    />,
  );
  expect(
    screen.getByText(/Refresh the page before editing this rule/),
  ).toBeTruthy();
  expect(
    screen
      .getByRole("button", { name: "Save changes" })
      .hasAttribute("disabled"),
  ).toBe(true);
  expect(screen.queryByText(/Current usage this future_window/)).toBeNull();
});
