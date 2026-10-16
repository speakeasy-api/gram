import { unrecognized } from "@gram/client/types/unrecognized.js";
import {
  cleanup,
  render,
  renderHook,
  screen,
  act,
  fireEvent,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { RuleDetailSheet } from "./RuleDetailSheet";
import { RuleSheet } from "./RuleSheet";
import {
  defaultRuleDraft,
  toDraft,
  targetSummary,
  type SpendRule,
} from "./budgets-data";
import {
  usePreviewBudgetRule,
  useBudgetRules,
  useCreateBudgetRule,
  useUpdateBudgetRule,
} from "./budgets-queries";

const mocks = vi.hoisted(() => ({ mutate: vi.fn(), rules: [] as SpendRule[] }));
vi.mock("@gram/client/react-query/spendRulesListRules.js", () => ({
  useSpendRulesListRules: () => ({
    data: { rules: mocks.rules },
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/spendRulesCreateRule.js", () => ({
  useSpendRulesCreateRuleMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/spendRulesUpdateRule.js", () => ({
  useSpendRulesUpdateRuleMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
  }),
}));
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

const unsupportedTarget = {
  attribute: "department_name",
  operator: "future_comparison",
  value: "Engineering",
};

it("preserves an unknown target through query mapping and edit initialization", () => {
  mocks.rules = [rule({ target: unsupportedTarget })];
  const { result } = renderHook(() => useBudgetRules());
  const mapped = result.current.rules[0]!;
  expect(mapped.target).toEqual(unsupportedTarget);
  expect(toDraft(mapped).target).toEqual(unsupportedTarget);
  expect(targetSummary(mapped.target)).toBe("Unsupported target condition");
});

it("refuses unsupported target writes and previews, but allows unrelated patches", () => {
  const onError = vi.fn<(message: string) => void>();
  const { result } = renderHook(() => ({
    preview: usePreviewBudgetRule(),
    create: useCreateBudgetRule({ onError }),
    update: useUpdateBudgetRule({ onError }),
  }));
  const draft = toDraft(rule({ target: unsupportedTarget }));
  act(() => {
    result.current.preview.preview(draft);
    result.current.create.create(draft);
    result.current.update.update({ id: "rule-id", ...draft });
  });
  expect(mocks.mutate).not.toHaveBeenCalled();
  expect(onError).toHaveBeenCalledTimes(2);
  act(() => result.current.update.update({ id: "rule-id", enabled: false }));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        updateSpendRuleRequestBody: expect.objectContaining({
          id: "rule-id",
          enabled: false,
        }),
      },
    }),
  );
});

it("shows unsupported target details without previewing or claiming no members match", () => {
  render(
    <RuleDetailSheet
      rule={rule({ target: unsupportedTarget })}
      usage={undefined}
      onClose={vi.fn<() => void>()}
      onEdit={vi.fn<() => void>()}
    />,
  );
  expect(
    screen.getByText(/Usage preview is unavailable for this target condition/),
  ).toBeTruthy();
  expect(screen.queryByText(/No members match/)).toBeNull();
  expect(mocks.mutate).not.toHaveBeenCalled();
});

it("blocks edits and previews for an unsupported stored target", () => {
  vi.useFakeTimers();
  try {
    const onSubmit = vi.fn<() => void>();
    render(
      <RuleSheet
        open
        onOpenChange={vi.fn<() => void>()}
        rule={rule({ target: unsupportedTarget })}
        onSubmit={onSubmit}
      />,
    );
    expect(
      screen.getByText(/This target condition is not supported/),
    ).toBeTruthy();
    expect(screen.getByText("Usage preview unavailable")).toBeTruthy();
    expect(
      screen.queryByRole("textbox", { name: "Condition value" }),
    ).toBeNull();
    const save = screen.getByRole("button", { name: "Save changes" });
    expect(save.hasAttribute("disabled")).toBe(true);
    fireEvent.click(save);
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(onSubmit).not.toHaveBeenCalled();
    expect(mocks.mutate).not.toHaveBeenCalled();
  } finally {
    vi.useRealTimers();
  }
});
