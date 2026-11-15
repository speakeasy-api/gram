import type { OrganizationUserSessionIssuerMigratePreflight } from "@gram/client/models/components/organizationusersessionissuermigratepreflight.js";
import type { UserSessionIssuerMigrationServerImpact } from "@gram/client/models/components/usersessionissuermigrationserverimpact.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { ConsolidateIssuerDialog } from "./ConsolidateIssuerDialog";

const testState = vi.hoisted(() => ({
  preflight: undefined as
    | OrganizationUserSessionIssuerMigratePreflight
    | undefined,
  mutate: vi.fn(),
  reset: vi.fn(),
  preflightError: false,
  preflightErrorValue: null as Error | null,
  migrateError: null as Error | null,
  preflightKeys: [] as unknown[][],
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));

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
  // Only the preview uses a plain query; record its key so a test can check
  // which target it previews.
  useQuery: (options: { queryKey: unknown[]; enabled?: boolean }) => {
    testState.preflightKeys.push(options.queryKey);
    return {
      data: options.enabled === false ? undefined : testState.preflight,
      isFetching: false,
      isError: testState.preflightError,
      error: testState.preflightErrorValue,
      refetch: vi.fn(),
    };
  },
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
    impactPreviewed: true,
    serverImpacts: [],
    droppedClientLinks: [],
    unscopedSessionCount: 0,
    newlyReachableServerCount: 0,
    ...overrides,
  };
}

function serverImpact(
  overrides: Partial<UserSessionIssuerMigrationServerImpact>,
): UserSessionIssuerMigrationServerImpact {
  return {
    mcpServerId: "server-1",
    mcpServerName: "Slack",
    mcpServerSlug: "slack",
    projectId: "project-1",
    projectName: "Sales",
    impactSeverity: "warning",
    reasons: ["changes_provider"],
    clientsBefore: [
      {
        remoteSessionClientId: "client-1",
        clientId: "slack-app",
        providerIssuer: "https://slack.com",
      },
    ],
    clientsAfter: [
      {
        remoteSessionClientId: "client-2",
        clientId: "google-app",
        providerIssuer: "https://accounts.google.com",
      },
    ],
    pinnedClientIds: [],
    hosted: false,
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
  testState.preflightErrorValue = null;
  testState.preflightKeys = [];
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

  it("previews each target it is pointed at", () => {
    testState.preflight = preflight({});
    render(
      <ConsolidateIssuerDialog
        source={issuer("source")}
        candidates={[issuer("first"), issuer("second")]}
        onClose={() => {}}
      />,
    );
    fireEvent.change(screen.getByLabelText("Consolidate into"), {
      target: { value: "first" },
    });
    expect(testState.preflightKeys.at(-1)).toEqual(
      expect.arrayContaining(["source", "first"]),
    );
    fireEvent.change(screen.getByLabelText("Consolidate into"), {
      target: { value: "second" },
    });
    expect(testState.preflightKeys.at(-1)).toEqual(
      expect.arrayContaining(["source", "second"]),
    );
  });

  it("confirms the MCP server changes it shows", () => {
    testState.preflight = preflight({
      serverImpacts: [serverImpact({})],
      warningsFingerprint: "fingerprint-servers",
    });
    renderDialog();

    expect(
      screen.getByText(/would change which accounts people connect/),
    ).toBeDefined();
    expect(
      screen.getByText(/Would switch from slack.com to accounts.google.com/),
    ).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Consolidate" }));
    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        migrateIssuerRequestBody2: {
          sourceId: "source",
          targetId: "target",
          confirmedWarningsFingerprint: "fingerprint-servers",
        },
      },
    });
  });

  it("lists the OAuth client IDs under technical details", () => {
    testState.preflight = preflight({
      serverImpacts: [serverImpact({})],
      droppedClientLinks: [
        {
          remoteSessionClientId: "client-3",
          clientId: "unused-app",
          providerIssuer: "https://mcp.notion.com",
        },
      ],
      warningsFingerprint: "fingerprint-details",
    });
    renderDialog();

    expect(screen.queryByText(/slack-app \(slack.com\)/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Technical details" }));
    expect(screen.getByText(/Now: slack-app \(slack.com\)/)).toBeDefined();
    expect(
      screen.getByText(/After: google-app \(accounts.google.com\)/),
    ).toBeDefined();
    expect(screen.getByText(/unused-app \(mcp.notion.com\)/)).toBeDefined();
  });

  it("refuses while an MCP server would get a client its upstream rejects", () => {
    testState.preflight = preflight({
      canMigrate: false,
      serverImpacts: [
        serverImpact({
          impactSeverity: "blocker",
          reasons: ["provider_not_in_metadata", "gains_client"],
          clientsBefore: [],
        }),
      ],
    });
    renderDialog();

    expect(
      screen.getByText(
        /Slack \(Sales\): Would use an OAuth client for accounts.google.com, which its upstream doesn't accept/,
      ),
    ).toBeDefined();
    expect(
      screen
        .getByRole("button", { name: "Consolidate" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  it("tells an admin to remove a binding for a blocked hosted server", () => {
    testState.preflight = preflight({
      canMigrate: false,
      serverImpacts: [
        serverImpact({
          hosted: true,
          impactSeverity: "blocker",
          reasons: ["multiple_clients_for_provider", "changes_clients"],
          clientsAfter: [
            {
              remoteSessionClientId: "client-1",
              clientId: "slack-app",
              providerIssuer: "https://slack.com",
            },
            {
              remoteSessionClientId: "client-2",
              clientId: "slack-app-2",
              providerIssuer: "https://slack.com",
            },
          ],
        }),
      ],
    });
    renderDialog();

    expect(
      screen.getByText(
        /more than one OAuth client for slack.com\. Remove the extra OAuth client/,
      ),
    ).toBeDefined();
  });

  it("names the OAuth clients that won't move and the servers that keep theirs", () => {
    testState.preflight = preflight({
      droppedClientLinks: [
        {
          remoteSessionClientId: "client-3",
          clientId: "unused-app",
          providerIssuer: "https://mcp.notion.com",
        },
      ],
      serverImpacts: [
        serverImpact({
          mcpServerName: "Linear",
          impactSeverity: "info",
          reasons: ["pinned"],
          clientsAfter: [],
          pinnedClientIds: ["client-1"],
        }),
      ],
      unscopedSessionCount: 4,
      newlyReachableServerCount: 2,
      warningsFingerprint: "fingerprint-dropped",
    });
    renderDialog();

    expect(
      screen.getByText(/1 OAuth client for mcp.notion.com won't move/),
    ).toBeDefined();
    expect(
      screen.getByText(/Linear \(Sales\) keeps its current OAuth client/),
    ).toBeDefined();
    expect(
      screen.getByText(
        /4 sign-ins will also work on the target's 2 MCP servers/,
      ),
    ).toBeDefined();
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

  it("says when another change holds the preview's locks", () => {
    testState.preflight = preflight({});
    testState.preflightError = true;
    testState.preflightErrorValue = new GramError("busy", {
      response: new Response(null, { status: 409 }),
      request: new Request("https://example.com"),
      body: "",
    });
    renderDialog();

    expect(
      screen.getByText(/Another change to these authorization servers/),
    ).toBeDefined();
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
    testState.migrateError = new Error("failed");
    testState.reset.mockImplementation(() => {
      testState.migrateError = null;
    });
    render(
      <ConsolidateIssuerDialog
        source={issuer("source")}
        candidates={[issuer("target"), issuer("other")]}
        onClose={() => {}}
      />,
    );
    expect(screen.getByText("Couldn't consolidate. Try again.")).toBeDefined();

    fireEvent.change(screen.getByLabelText("Consolidate into"), {
      target: { value: "other" },
    });
    expect(screen.queryByText("Couldn't consolidate. Try again.")).toBeNull();
  });

  it("says when the preview went stale", () => {
    testState.preflight = preflight({});
    testState.migrateError = Object.assign(
      Object.create(GramError.prototype) as GramError,
      { statusCode: 409, message: "conflict" },
    );
    renderDialog();

    expect(
      screen.getByText(/Something changed since this preview/),
    ).toBeDefined();
  });

  it("names everything that moves", () => {
    testState.preflight = preflight({
      clientCount: 2,
      sessionCount: 3,
      consentCount: 4,
      cimdClientCount: 1,
      remoteSessionCount: 1,
    });
    renderDialog();

    expect(
      screen.getByText(
        "2 clients, 3 sessions, 4 sign-in approvals, 1 allowed client document and 1 linked account move to the target.",
      ),
    ).toBeDefined();
  });

  it("offers nothing until every candidate loads", () => {
    const retry = vi.fn();
    render(
      <ConsolidateIssuerDialog
        source={issuer("source")}
        candidates={[issuer("target")]}
        onRetryCandidates={() => {
          retry();
        }}
        onClose={() => {}}
      />,
    );

    expect(screen.queryByLabelText("Consolidate into")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalled();
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

    expect(
      screen.getByRole("status", { name: "Loading authorization servers" }),
    ).toBeDefined();
    expect(screen.queryByText(/No other authorization server/)).toBeNull();
  });
});
