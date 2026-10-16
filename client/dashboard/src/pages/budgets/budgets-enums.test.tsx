import userEvent from "@testing-library/user-event";
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
  matchesRuleSearch,
  type ActorAttribute,
  type SpendRule,
} from "./budgets-data";
import {
  usePreviewBudgetRule,
  useBudgetRules,
  useCreateBudgetRule,
  useUpdateBudgetRule,
  useActorAttributes,
} from "./budgets-queries";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  rules: [] as SpendRule[],
  catalog: { data: { attributes: [] }, isError: false } as {
    data: { attributes: ActorAttribute[] } | undefined;
    isError: boolean;
  },
}));
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
  useSpendRulesActorAttributes: () => mocks.catalog,
}));

afterEach(cleanup);
beforeEach(() => {
  mocks.mutate.mockClear();
  mocks.rules = [];
  mocks.catalog = {
    data: {
      attributes: [
        { name: "department_name", type: "string", description: "Department" },
      ],
    },
    isError: false,
  };
});

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

it("preserves explicitly unsupported attribute types in the catalog", () => {
  mocks.catalog.data!.attributes.push({
    name: "future_attribute",
    type: unrecognized("future_type"),
    description: "Future attribute",
  });
  const { result } = renderHook(() => useActorAttributes());
  expect(result.current.attributes).toEqual(mocks.catalog.data!.attributes);
});

it.each([
  {
    name: "unknown type",
    data: {
      attributes: [
        {
          name: "department_name",
          type: "future_type",
          description: "Department",
        },
      ],
    },
    isError: false,
  },
  { name: "unloaded catalog", data: undefined, isError: false },
  { name: "failed catalog", data: undefined, isError: true },
])("blocks preview and target writes for $name", ({ data, isError }) => {
  mocks.catalog = { data, isError };
  const { result } = renderHook(() => ({
    create: useCreateBudgetRule(),
    update: useUpdateBudgetRule(),
    preview: usePreviewBudgetRule(),
  }));
  const draft = toDraft(rule());
  act(() => {
    result.current.create.create(draft);
    result.current.update.update({ id: "rule-id", ...draft });
    result.current.preview.preview(draft);
  });
  expect(mocks.mutate).not.toHaveBeenCalled();
  act(() => result.current.update.update({ id: "rule-id", enabled: false }));
  expect(mocks.mutate).toHaveBeenCalledTimes(1);
});

it.each([
  {
    name: "known string",
    attributes: [
      { name: "department_name", type: "string", description: "Department" },
    ],
    operator: "equals",
  },
  {
    name: "known list",
    attributes: [
      { name: "department_name", type: "list", description: "Departments" },
    ],
    operator: "includes",
  },
  {
    name: "custom attribute absent from catalog",
    attributes: [{ name: "email", type: "string", description: "Email" }],
    operator: "equals",
  },
  { name: "empty catalog", attributes: [], operator: "equals" },
])("allows preview and target writes for $name", ({ attributes, operator }) => {
  mocks.catalog = { data: { attributes }, isError: false };
  const { result } = renderHook(() => ({
    create: useCreateBudgetRule(),
    update: useUpdateBudgetRule(),
    preview: usePreviewBudgetRule(),
  }));
  const draft = toDraft(rule({ target: { ...rule().target, operator } }));
  act(() => {
    result.current.create.create(draft);
    result.current.update.update({ id: "rule-id", ...draft });
    result.current.preview.preview(draft);
  });
  expect(mocks.mutate).toHaveBeenCalledTimes(3);
  expect(mocks.mutate.mock.calls[0]![0]).toEqual(
    expect.objectContaining({
      request: {
        createSpendRuleRequestBody: expect.objectContaining({
          target: draft.target,
        }),
      },
    }),
  );
});

it("keeps an explicitly unsupported attribute read-only in the editor", () => {
  mocks.catalog.data!.attributes[0]!.type = "future_type";
  render(
    <RuleSheet
      open
      onOpenChange={vi.fn<() => void>()}
      rule={rule()}
      onSubmit={vi.fn<() => void>()}
    />,
  );
  expect(
    screen.getByText(/This target condition is not supported/),
  ).toBeTruthy();
  expect(screen.queryByRole("textbox", { name: "Condition value" })).toBeNull();
  expect(screen.getByText("Usage preview unavailable")).toBeTruthy();
  expect(
    screen
      .getByRole("button", { name: "Save changes" })
      .hasAttribute("disabled"),
  ).toBe(true);
  expect(mocks.mutate).not.toHaveBeenCalled();
});

it("waits for the catalog then enables a known target without rewriting it", () => {
  mocks.catalog = { data: undefined, isError: false };
  const onSubmit = vi.fn<() => void>();
  const props = {
    open: true,
    onOpenChange: vi.fn<() => void>(),
    rule: rule(),
    onSubmit,
  };
  const { rerender } = render(<RuleSheet {...props} />);
  expect(screen.getByText("Loading target attributes…")).toBeTruthy();
  expect(
    screen
      .getByRole("button", { name: "Save changes" })
      .hasAttribute("disabled"),
  ).toBe(true);
  expect(mocks.mutate).not.toHaveBeenCalled();
  mocks.catalog = {
    data: {
      attributes: [
        { name: "department_name", type: "string", description: "Department" },
      ],
    },
    isError: false,
  };
  rerender(<RuleSheet {...props} />);
  expect(screen.queryByText("Loading target attributes…")).toBeNull();
  expect(screen.getByRole("textbox", { name: "Condition value" })).toBeTruthy();
  const save = screen.getByRole("button", { name: "Save changes" });
  expect(save.hasAttribute("disabled")).toBe(false);
  fireEvent.click(save);
  expect(onSubmit).toHaveBeenCalledWith(toDraft(props.rule));
});

it("retains attribute and value search for targets with unsupported operators", () => {
  const stored = rule({ target: unsupportedTarget });
  expect(targetSummary(stored.target)).toBe("Unsupported target condition");
  expect(matchesRuleSearch(stored, " ENGINEERING ")).toBe(true);
  expect(matchesRuleSearch(stored, "department_name")).toBe(true);
  expect(matchesRuleSearch(stored, "Finance")).toBe(false);
});

it.each([
  { type: "string", operator: "equals" },
  { type: "list", operator: "includes" },
])("keeps known $type target edits saveable", ({ type, operator }) => {
  mocks.catalog.data!.attributes[0]!.type = type;
  const stored = rule({
    action: "flag",
    target: { ...rule().target, operator },
  });
  const onSubmit = vi.fn<() => void>();
  render(
    <RuleSheet
      open
      onOpenChange={vi.fn<() => void>()}
      rule={stored}
      onSubmit={onSubmit}
    />,
  );
  fireEvent.change(screen.getByRole("textbox", { name: "Condition value" }), {
    target: { value: "Support" },
  });
  const save = screen.getByRole("button", { name: "Save changes" });
  expect(save.hasAttribute("disabled")).toBe(false);
  fireEvent.click(save);
  expect(onSubmit).toHaveBeenCalledWith(
    expect.objectContaining({
      target: { attribute: "department_name", operator, value: "Support" },
    }),
  );
});

it.each([false, true])(
  "prioritizes an unknown window over an unverified catalog (error=%s)",
  (isError) => {
    mocks.catalog = { data: undefined, isError };
    render(
      <RuleDetailSheet
        rule={rule({ windowKind: unrecognized("future_window") })}
        usage={undefined}
        onClose={vi.fn<() => void>()}
        onEdit={vi.fn<() => void>()}
      />,
    );
    expect(
      screen.getByText(/Usage preview is unavailable for this window/),
    ).toBeTruthy();
    expect(screen.queryByText("Loading target attributes…")).toBeNull();
    expect(screen.queryByText(/Could not load target attributes/)).toBeNull();
    expect(mocks.mutate).not.toHaveBeenCalled();
  },
);

it.each([false, true])(
  "describes an unverified catalog accurately in mutation callbacks (error=%s)",
  (isError) => {
    mocks.catalog = { data: undefined, isError };
    const onError = vi.fn<(message: string) => void>();
    const { result } = renderHook(() => ({
      create: useCreateBudgetRule({ onError }),
      update: useUpdateBudgetRule({ onError }),
    }));
    act(() => {
      result.current.create.create(toDraft(rule()));
      result.current.update.update({ id: "rule-id", target: rule().target });
    });
    expect(onError).toHaveBeenCalledTimes(2);
    expect(onError).toHaveBeenNthCalledWith(
      1,
      "Target attributes have not been verified. Refresh the page before editing this target condition.",
    );
    expect(onError).toHaveBeenNthCalledWith(
      2,
      "Target attributes have not been verified. Refresh the page before editing this target condition.",
    );
    expect(mocks.mutate).not.toHaveBeenCalled();
  },
);

it("still identifies an explicitly unsupported target in mutation callbacks", () => {
  mocks.catalog.data!.attributes[0]!.type = "future_type";
  const onError = vi.fn<(message: string) => void>();
  const { result } = renderHook(() => useUpdateBudgetRule({ onError }));
  act(() => result.current.update({ id: "rule-id", target: rule().target }));
  expect(onError).toHaveBeenCalledWith(
    "Refresh the page before editing this unsupported target condition.",
  );
  expect(mocks.mutate).not.toHaveBeenCalled();
});

it("displays and preserves a saved includes operator for an attribute absent from the catalog", async () => {
  const user = userEvent.setup();
  const stored = rule({
    action: "flag",
    target: {
      attribute: "custom_groups",
      operator: "includes",
      value: "Engineering",
    },
  });
  const onSubmit = vi.fn<() => void>();
  render(
    <RuleSheet
      open
      onOpenChange={vi.fn<() => void>()}
      rule={stored}
      onSubmit={onSubmit}
    />,
  );
  const selects = screen.getAllByRole("combobox");
  expect(selects[0]!.textContent).toContain("Custom Groups");
  expect(selects[1]!.textContent).toContain("includes");
  await user.click(selects[1]!);
  await user.click(screen.getByRole("option", { name: "includes" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Condition value" }), {
    target: { value: "Support" },
  });
  const save = screen.getByRole("button", { name: "Save changes" });
  expect(save.hasAttribute("disabled")).toBe(false);
  fireEvent.click(save);
  expect(onSubmit).toHaveBeenCalledWith(
    expect.objectContaining({
      target: {
        attribute: "custom_groups",
        operator: "includes",
        value: "Support",
      },
    }),
  );
});
