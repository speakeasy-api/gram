import {
  cleanup,
  render,
  screen,
  fireEvent,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Convergence, ConvergenceHelp, MigrationReview } from "./Convergence";
const api = vi.hoisted(() => ({
  preflight: vi.fn(),
  migrate: vi.fn(),
  candidates: vi.fn(),
}));
vi.mock("@/lib/gramAdminClient", () => ({
  adminListGlobalIssuerConvergenceCandidatesQuery: (request: unknown) => ({
    queryKey: ["candidates", request],
    queryFn: () => api.candidates(request),
  }),
  adminGetGlobalIssuerMigratePreflightQuery: (r: unknown) => ({
    queryKey: ["preflight", r],
    queryFn: () => api.preflight(r),
  }),
  adminMigrateToGlobalIssuer: api.migrate,
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
function mount() {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MigrationReview
        sourceId="source"
        targetId="target"
        onClose={vi.fn<() => void>()}
      />
    </QueryClientProvider>,
  );
}
it("blocks confirmation after failed authoritative preflight", async () => {
  api.preflight.mockRejectedValue(new Error("Preflight unavailable"));
  mount();
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Preflight unavailable",
  );
  expect(
    (screen.getByRole("button", { name: "Consolidate" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
});
it("shows conflicting bindings and refuses unsafe migration", async () => {
  api.preflight.mockResolvedValue({
    canMigrate: false,
    clientCount: 1,
    targetTenantClientCount: 2,
    mcpServerNames: ["Example MCP"],
    conflictingMcpServerNames: ["Conflicting MCP"],
    endpointMismatches: [],
    warnings: [],
  });
  mount();
  expect(await screen.findByText("Conflicting MCP")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Consolidate" }));
  expect(api.migrate).not.toHaveBeenCalled();
});

it("allows warnings only after preflight and reports migration races", async () => {
  api.preflight.mockResolvedValue({
    canMigrate: true,
    clientCount: 1,
    targetTenantClientCount: 0,
    mcpServerNames: [],
    conflictingMcpServerNames: [],
    endpointMismatches: [],
    warnings: [
      {
        field: "scopes_supported",
        sourceValues: ["old"],
        targetValues: ["new"],
      },
    ],
  });
  api.migrate.mockRejectedValue(new Error("Binding changed during migration"));
  mount();
  expect(await screen.findByText("Scopes")).toBeTruthy();
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Consolidate" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false),
  );
  fireEvent.click(screen.getByRole("button", { name: "Consolidate" }));
  expect((await screen.findByRole("alert")).textContent).toContain(
    "Binding changed during migration",
  );
  expect(api.migrate).toHaveBeenCalledWith({
    sourceId: "source",
    targetId: "target",
  });
  await waitFor(() => expect(api.preflight).toHaveBeenCalledTimes(2));
});

it("explains convergence on keyboard focus and dismisses on Escape", async () => {
  render(
    <TooltipProvider>
      <ConvergenceHelp />
    </TooltipProvider>,
  );
  const trigger = screen.getByRole("button", { name: "About convergence" });
  trigger.focus();
  expect((await screen.findByRole("tooltip")).textContent).toContain(
    "Organization or project issuers that use the same upstream identity provider",
  );
  fireEvent.keyDown(trigger, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
});
it("discloses named target, fallback owner, scope deltas and scalar empty values", async () => {
  api.preflight.mockResolvedValue({
    canMigrate: false,
    clientCount: 2,
    targetTenantClientCount: 1,
    mcpServerNames: ["Affected server"],
    conflictingMcpServerNames: ["Conflicting binding"],
    endpointMismatches: [
      { field: "token_endpoint", sourceValue: "", targetValue: undefined },
    ],
    warnings: [
      {
        field: "scopes_supported",
        sourceValues: ["old", "kept"],
        targetValues: ["new", "kept"],
      },
    ],
  });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MigrationReview
        sourceId="source"
        sourceOwner="organization-fallback"
        targetId="target"
        targetName="Named platform provider"
        onClose={() => {}}
      />
    </QueryClientProvider>,
  );
  expect(await screen.findByText("Added: new")).toBeTruthy();
  expect(screen.getByText("Dropped: old")).toBeTruthy();
  expect(screen.getByText(/owned by organization-fallback/)).toBeTruthy();
  expect(screen.getByText(/Named platform provider/)).toBeTruthy();
  expect(screen.getByText("Affected server")).toBeTruthy();
  expect(screen.getByText("Conflicting binding")).toBeTruthy();
  expect(screen.getByText(/Source: empty/)).toBeTruthy();
});
it("carries an unsynced organization ID fallback from candidate row into review", async () => {
  api.candidates.mockResolvedValue({
    result: {
      items: [
        {
          issuer: {
            id: "source",
            name: "Source provider",
            issuer: "https://source.example",
            projectId: "",
          },
          organizationName: "",
          organizationId: "owner-fallback",
          clientCount: 1,
          endpointMismatches: [],
          warnings: [],
        },
      ],
    },
  });
  api.preflight.mockResolvedValue({
    canMigrate: true,
    clientCount: 1,
    targetTenantClientCount: 0,
    mcpServerNames: [],
    conflictingMcpServerNames: [],
    endpointMismatches: [],
    warnings: [],
  });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <Convergence issuerId="target" targetName="Named target" />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(await screen.findByRole("button", { name: "Review" }));
  expect(await screen.findByText(/owned by owner-fallback/)).toBeTruthy();
  expect(screen.getByText(/platform issuer Named target/)).toBeTruthy();
});

it.each([undefined, "", "   "])(
  "identifies a nameless source (%j) by issuer URL while preserving migration IDs",
  async (name) => {
    api.candidates.mockResolvedValue({
      result: {
        items: [
          {
            issuer: {
              id: "source-id",
              name,
              issuer: "https://source.example",
              projectId: "",
            },
            organizationName: "Source owner",
            organizationId: "owner-id",
            clientCount: 1,
            endpointMismatches: [],
            warnings: [],
          },
        ],
      },
    });
    api.preflight.mockResolvedValue({
      canMigrate: true,
      clientCount: 1,
      targetTenantClientCount: 0,
      mcpServerNames: [],
      conflictingMcpServerNames: [],
      endpointMismatches: [],
      warnings: [],
    });
    api.migrate.mockResolvedValue({});
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <Convergence issuerId="target-id" targetName="Named target" />
        </TooltipProvider>
      </QueryClientProvider>,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Review" }));
    expect(
      await screen.findByRole("heading", {
        name: "Consolidate https://source.example",
      }),
    ).toBeTruthy();
    expect(
      screen.getByText(/Move clients from https:\/\/source.example/),
    ).toBeTruthy();
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: "Consolidate",
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    fireEvent.click(screen.getByRole("button", { name: "Consolidate" }));
    await waitFor(() =>
      expect(api.migrate).toHaveBeenCalledWith({
        sourceId: "source-id",
        targetId: "target-id",
      }),
    );
  },
);

it("resets pagination and closes migration review when the target changes", async () => {
  api.candidates.mockResolvedValue({
    result: {
      items: [
        {
          issuer: {
            id: "source",
            name: "Source provider",
            issuer: "https://source.example",
          },
          organizationId: "owner",
          clientCount: 1,
          endpointMismatches: [],
          warnings: [],
        },
      ],
      nextCursor: "target-a-page-2",
    },
  });
  api.preflight.mockResolvedValue({
    canMigrate: true,
    clientCount: 1,
    targetTenantClientCount: 0,
    mcpServerNames: [],
    conflictingMcpServerNames: [],
    endpointMismatches: [],
    warnings: [],
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = (issuerId: string) => (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <Convergence issuerId={issuerId} />
      </TooltipProvider>
    </QueryClientProvider>
  );
  const { rerender } = render(view("target-a"));
  await screen.findByRole("button", { name: "Review" });
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  await waitFor(() =>
    expect(api.candidates).toHaveBeenCalledWith({
      targetId: "target-a",
      cursor: "target-a-page-2",
      limit: 50,
    }),
  );
  fireEvent.click(await screen.findByRole("button", { name: "Review" }));
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Consolidate" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false),
  );
  rerender(view("target-b"));
  expect(screen.queryByRole("dialog")).toBeNull();
  await screen.findByRole("button", { name: "Review" });
  expect(
    (screen.getByRole("button", { name: "Previous" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(api.candidates).toHaveBeenLastCalledWith({
    targetId: "target-b",
    cursor: undefined,
    limit: 50,
  });
  expect(api.candidates).not.toHaveBeenCalledWith({
    targetId: "target-b",
    cursor: "target-a-page-2",
    limit: 50,
  });
  expect(api.preflight).not.toHaveBeenCalledWith({
    sourceId: "source",
    targetId: "target-b",
  });
  expect(api.migrate).not.toHaveBeenCalled();
});
