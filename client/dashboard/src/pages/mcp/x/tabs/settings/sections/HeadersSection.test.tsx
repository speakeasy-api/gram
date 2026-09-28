import {
  headerDraftErrors,
  type HeaderDraft,
  type HeaderDraftsState,
} from "@/lib/remote-identity";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { HeadersSection } from "./HeadersSection";

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
  }),
}));

function draft(overrides: Partial<HeaderDraft> = {}): HeaderDraft {
  return {
    key: "row-1",
    name: "X-Trace",
    source: "static",
    staticValue: "on",
    valueFromRequestHeader: "",
    isRequired: false,
    isSecret: false,
    hadSecret: false,
    ...overrides,
  };
}

function headerState(
  overrides: Partial<HeaderDraftsState> = {},
): HeaderDraftsState {
  return {
    drafts: [],
    authorization: { unknown: false },
    readOnly: false,
    isLoading: false,
    isDirty: false,
    validationError: null,
    fieldErrors: new Map(),
    reportErrors: false,
    saving: false,
    error: null,
    addHeader: vi.fn(() => {}),
    replaceHeader: vi.fn(() => {}),
    removeHeader: vi.fn(() => {}),
    save: vi.fn(async () => false),
    ...overrides,
  };
}

/** The app mounts one TooltipProvider at its root; a test has to supply it. */
function renderSection(state: HeaderDraftsState): void {
  render(
    <MemoryRouter>
      <TooltipProvider>
        <HeadersSection state={state} siblingMcpServers={[]} />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("HeadersSection", () => {
  it("shows Authorization as spoken for under User Identity", () => {
    renderSection(
      headerState({ authorization: { mode: "user", unknown: false } }),
    );

    // The point of the row: someone expanding this form sees the name is
    // taken, instead of an empty list that invites them to claim it.
    const name = screen.getByDisplayValue("Authorization");
    expect((name as HTMLInputElement).disabled).toBe(true);
    const value = screen.getByPlaceholderText(
      "Filled by the identity provider",
    );
    expect((value as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByLabelText("Managed by User Identity")).toBeTruthy();
    expect(
      screen.getByText("No other upstream headers configured yet."),
    ).toBeTruthy();
  });

  it("stands in only while nothing has actually been written", () => {
    // A saved static credential under User Identity is a leftover to clean up,
    // not a slot being filled — it gets the real row and its own wording.
    renderSection(
      headerState({
        authorization: {
          mode: "user",
          managedHeaderId: "header-authorization",
          unknown: false,
        },
        drafts: [
          draft({
            key: "row-authorization",
            id: "header-authorization",
            name: "Authorization",
            staticValue: "***",
          }),
        ],
      }),
    );

    expect(screen.getByLabelText("Disabled by User Identity")).toBeTruthy();
    expect(
      screen.queryByPlaceholderText("Filled by the identity provider"),
    ).toBeNull();
  });

  it("gives every row the same cells, so the columns line up", () => {
    renderSection(
      headerState({
        authorization: {
          mode: "agent",
          managedHeaderId: "header-authorization",
          unknown: false,
        },
        drafts: [
          draft({
            key: "row-authorization",
            id: "header-authorization",
            name: "Authorization",
            staticValue: "***",
          }),
          draft({ key: "row-trace", id: "header-trace", name: "X-Trace" }),
        ],
      }),
    );

    // Grid children are placed in order, so a cell skipped on the managed row
    // — no flags, no delete — would pull the lock into the flags column and
    // knock that row's fields out of line with the others.
    const rows = document.querySelectorAll('[data-slot="header-row"]');
    expect(rows).toHaveLength(2);
    for (const row of rows) {
      expect(row.children).toHaveLength(5);
    }
  });

  it("marks the field that is wrong, not the whole row", () => {
    const rows = [
      draft({ key: "row-nameless", name: "", staticValue: "set" }),
      draft({ key: "row-valueless", name: "X-Api-Key", staticValue: "" }),
    ];
    renderSection(
      headerState({
        drafts: rows,
        fieldErrors: headerDraftErrors(rows),
        reportErrors: true,
      }),
    );

    // Input puts className on the bordered box around the field, not the
    // field itself.
    const box = (field: HTMLElement): string =>
      field.parentElement?.className ?? "";
    const names = screen.getAllByLabelText("Header name");
    const values = screen.getAllByLabelText("Header value");

    expect(box(names[0]!)).toContain("border-warning-default");
    expect(box(values[0]!)).not.toContain("border-warning-default");
    expect(box(names[1]!)).not.toContain("border-warning-default");
    expect(box(values[1]!)).toContain("border-warning-default");
  });

  it("stays quiet about problems nobody has been told about yet", () => {
    const rows = [draft({ key: "row-nameless", name: "" })];
    renderSection(
      headerState({
        drafts: rows,
        fieldErrors: headerDraftErrors(rows),
        reportErrors: false,
      }),
    );

    const name = screen.getByLabelText("Header name");
    expect(name.parentElement?.className ?? "").not.toContain(
      "border-warning-default",
    );
  });

  it("leaves the name free when no identity claims it", () => {
    renderSection(
      headerState({ authorization: { mode: "none", unknown: false } }),
    );

    expect(screen.queryByDisplayValue("Authorization")).toBeNull();
    expect(
      screen.getByText("No upstream headers configured yet."),
    ).toBeTruthy();
  });
});
