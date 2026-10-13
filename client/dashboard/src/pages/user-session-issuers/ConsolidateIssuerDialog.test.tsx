import type { OrganizationUserSessionIssuerMigratePreflight } from "@gram/client/models/components/organizationusersessionissuermigratepreflight.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConsolidateIssuerDialog } from "./ConsolidateIssuerDialog";

const testState = vi.hoisted(() => ({
  preflight: undefined as
    | OrganizationUserSessionIssuerMigratePreflight
    | undefined,
  mutate: vi.fn(),
  reset: vi.fn(),
  preflightError: false,
  migrateError: null as Error | null,
}));

vi.mock(
  "@gram/client/react-query/organizationUserSessionIssuerMigratePreflight.js",
  () => ({
    useOrganizationUserSessionIssuerMigratePreflight: () => ({
      data: testState.preflight,
      isFetching: false,
      isError: testState.preflightError,
      refetch: vi.fn(),
    }),
  }),
);

vi.mock(
  "@gram/client/react-query/migrateOrganizationUserSessionIssuer.js",
  () => ({
    useMigrateOrganizationUserSessionIssuerMutation: () => ({
      mutate: testState.mutate,
      reset: testState.reset,
      isPending: false,
      isError: testState.migrateError !== null,
      error: testState.migrateError,
    }),
  }),
);

vi.mock("./invalidateIssuers", () => ({ invalidateIssuers: vi.fn() }));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

// The real select is a Radix popover; a native one keeps the test on the
// dialog's flow.
vi.mock("@/components/ui/Select", () => ({
  Select: ({
    value,
    onValueChange,
    children,
  }: {
    value: string;
    onValueChange: (value: string) => void;
    children: React.ReactNode;
  }) => (
    <select
      aria-label="Consolidate into"
      value={value}
      onChange={(event) => onValueChange(event.target.value)}
    >
      <option value="">Choose</option>
      {children}
    </select>
  ),
  SelectTrigger: () => null,
  SelectValue: () => null,
  SelectContent: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  SelectItem: ({
    value,
    children,
  }: {
    value: string;
    children: React.ReactNode;
  }) => <option value={value}>{children}</option>,
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function issuer(id: string): UserSessionIssuer {
  return { id, slug: id, projectId: "" } as UserSessionIssuer;
}

function preflight(
  overrides: Partial<OrganizationUserSessionIssuerMigratePreflight>,
): OrganizationUserSessionIssuerMigratePreflight {
  return {
    canMigrate: true,
    platformOwned: false,
    clientCount: 2,
    sessionCount: 3,
    consentCount: 0,
    cimdClientCount: 0,
    remoteSessionCount: 0,
    conflictingClientIds: [],
    principalBindingConflictCount: 0,
    emaBindingConflictCount: 0,
    warnings: [],
    warningsFingerprint: "",
    ...overrides,
  };
}

function renderDialog(): void {
  render(
    <ConsolidateIssuerDialog
      source={issuer("source")}
      candidates={[issuer("target")]}
      onClose={() => {}}
    />,
  );
  fireEvent.change(screen.getByLabelText("Consolidate into"), {
    target: { value: "target" },
  });
}

beforeEach(() => {
  testState.mutate.mockReset();
  testState.reset.mockReset();
  testState.preflight = undefined;
  testState.preflightError = false;
  testState.migrateError = null;
});

afterEach(() => {
  cleanup();
});

describe("ConsolidateIssuerDialog", () => {
  it("confirms the warnings it shows", () => {
    testState.preflight = preflight({
      warnings: [
        {
          field: "issuer_url",
          sourceValue: "https://app.example.com/oauth/usi/source",
          targetValue: "",
        },
      ],
      warningsFingerprint: "fingerprint-1",
    });
    renderDialog();

    expect(screen.getByText(/update any identity provider/)).toBeDefined();
    expect(screen.getByText(/None, one per MCP server/)).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Consolidate" }));
    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        migrateIssuerRequestBody2: {
          sourceId: "source",
          targetId: "target",
          confirmedWarningsFingerprint: "fingerprint-1",
        },
      },
    });
  });

  it("refuses while something blocks the move", () => {
    testState.preflight = preflight({
      canMigrate: false,
      conflictingClientIds: ["shared-client"],
    });
    renderDialog();

    expect(screen.getByText(/registered with both/)).toBeDefined();
    expect(
      screen
        .getByRole("button", { name: "Consolidate" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  it("does not confirm a preview whose refresh failed", () => {
    testState.preflight = preflight({});
    testState.preflightError = true;
    renderDialog();

    expect(screen.getByText(/Couldn't check what consolidating/)).toBeDefined();
    expect(
      screen
        .getByRole("button", { name: "Consolidate" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  it("clears a refusal when another target is chosen", () => {
    testState.preflight = preflight({});
    testState.migrateError = new Error("conflict");
    renderDialog();

    expect(screen.getByText("Couldn't consolidate. Try again.")).toBeDefined();
    expect(testState.reset).toHaveBeenCalled();
  });

  it("waits for candidates before saying there are none", () => {
    render(
      <ConsolidateIssuerDialog
        source={issuer("source")}
        candidates={[]}
        candidatesLoading
        onClose={() => {}}
      />,
    );

    expect(screen.getByText("Loading authorization servers…")).toBeDefined();
    expect(screen.queryByText(/No other authorization server/)).toBeNull();
  });
});
