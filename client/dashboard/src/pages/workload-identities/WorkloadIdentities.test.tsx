import { cleanup, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { WorkloadIdentitiesPage } from "./WorkloadIdentities";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    children,
    primaryAction,
  }: {
    children: ReactNode;
    primaryAction: ReactNode;
  }) => (
    <>
      {primaryAction}
      {children}
    </>
  ),
}));

const FLEET_STEM = "wimse://identity.example.com/org/acme/agent/";

vi.mock("@gram/client/react-query/workloadIdentities.js", () => ({
  useWorkloadIdentities: () => ({
    data: {
      issuers: [
        {
          id: "issuer-1",
          organizationId: "example-org",
          projectId: "",
          name: "Example Platform",
          issuer: "https://identity.example.com",
          jwksUri: "https://identity.example.com/jwks",
          allowWildcardAdmission: true,
          createdAt: new Date("2026-09-23T00:00:00Z"),
          updatedAt: new Date("2026-09-23T00:00:00Z"),
        },
      ],
      admissions: [
        {
          id: "admission-active",
          organizationId: "example-org",
          projectId: "",
          workloadIssuerId: "issuer-1",
          issuer: "https://identity.example.com",
          issuerName: "Example Platform",
          subject: `${FLEET_STEM}*`,
          matchKind: "wildcard",
          name: "",
          agentId: "agent-1",
          agentName: "poc-agent",
          wildcardActive: true,
          createdAt: new Date("2026-09-23T00:00:00Z"),
          updatedAt: new Date("2026-09-23T00:00:00Z"),
        },
        {
          id: "admission-inert",
          organizationId: "example-org",
          projectId: "",
          workloadIssuerId: "issuer-1",
          issuer: "https://identity.example.com",
          issuerName: "Example Platform",
          subject: `${FLEET_STEM}other/*`,
          matchKind: "wildcard",
          name: "",
          agentId: "",
          agentName: "",
          wildcardActive: false,
          createdAt: new Date("2026-09-23T00:00:00Z"),
          updatedAt: new Date("2026-09-23T00:00:00Z"),
        },
      ],
    },
    isPending: false,
  }),
  invalidateAllWorkloadIdentities: vi.fn(),
}));
const agentsState = vi.hoisted<{
  data?: { id: string; name: string; lifecycle: string }[];
  isError: boolean;
}>(() => ({
  data: [{ id: "agent-1", name: "poc-agent", lifecycle: "active" }],
  isError: false,
}));
vi.mock("@gram/client/react-query/agents.js", () => ({
  useAgents: () => agentsState,
}));
vi.mock("@gram/client/react-query/registerWorkloadIssuer.js", () => ({
  useRegisterWorkloadIssuerMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/withdrawWorkloadIssuer.js", () => ({
  useWithdrawWorkloadIssuerMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/admitWorkloadSubject.js", () => ({
  useAdmitWorkloadSubjectMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/withdrawWorkloadSubject.js", () => ({
  useWithdrawWorkloadSubjectMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));

afterEach(() => {
  cleanup();
  agentsState.data = [
    { id: "agent-1", name: "poc-agent", lifecycle: "active" },
  ];
  agentsState.isError = false;
});

function renderPage(): void {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <WorkloadIdentitiesPage />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("lists the trusted issuers and the subjects admitted under them", () => {
  renderPage();

  // The issuer's name appears in its own row and in the issuer column of each
  // admission it vouches for, so a count is worth asserting rather than
  // uniqueness. The admit dialog's picker contributes nothing here: no test opens
  // the dialog, and Radix mounts its portal content only while open.
  expect(screen.getAllByText(/Example Platform/).length).toBeGreaterThan(1);
  expect(screen.getByText("https://identity.example.com")).toBeTruthy();
  expect(screen.getByText(`${FLEET_STEM}*`)).toBeTruthy();
  expect(screen.getByText(`${FLEET_STEM}other/*`)).toBeTruthy();
  expect(screen.getAllByText("poc-agent").length).toBeGreaterThan(0);

  // Organization tier, for both the issuer and the admissions.
  expect(screen.getAllByText("Organization").length).toBe(3);
});

it("distinguishes a wildcard rule that is no longer in force", () => {
  renderPage();

  // A rule written while the issuer permitted wildcards stays stored after the
  // permission is cleared and matches nothing. Shown as an ordinary wildcard it
  // would read as working configuration.
  expect(screen.getByText("Wildcard, inert")).toBeTruthy();
  expect(screen.getByText("Wildcard")).toBeTruthy();
});

it("names a subject with no assigned agent, which cannot authenticate", () => {
  renderPage();

  expect(screen.getByText("None assigned")).toBeTruthy();
});

it("still renders the policy when agents cannot be listed", () => {
  // The whole agents service 404s when the agent management rollout is off, and
  // the global query policy only suppresses 401 and 403 — so left to throw it
  // takes this page down even though the trust policy has loaded.
  agentsState.data = undefined;
  agentsState.isError = true;

  renderPage();

  expect(screen.getByText(`${FLEET_STEM}*`)).toBeTruthy();
  expect(
    screen.getByText(/Agent management is not enabled for this organization/),
  ).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Admit a workload" }),
  ).toHaveProperty("disabled", true);
});

it("offers admitting a workload when an active agent can back it", () => {
  renderPage();

  expect(
    screen.getByRole("button", { name: "Admit a workload" }),
  ).toHaveProperty("disabled", false);
});

it("says to create an agent when the organization has none", () => {
  agentsState.data = [];

  renderPage();

  expect(screen.getByText(/Create an agent first/)).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Admit a workload" }),
  ).toHaveProperty("disabled", true);
});

it("says to reactivate rather than create when every agent is inactive", () => {
  // Only active agents are assignable, so this organization has agents and
  // still nothing to offer. Telling it to create one sends it to make a second
  // agent instead of reactivating the one it has.
  agentsState.data = [
    { id: "agent-1", name: "poc-agent", lifecycle: "suspended" },
    { id: "agent-2", name: "old-agent", lifecycle: "revoked" },
  ];

  renderPage();

  expect(screen.getByText(/Every agent is suspended or revoked/)).toBeTruthy();
  expect(screen.queryByText(/Create an agent first/)).toBeNull();
  expect(
    screen.getByRole("button", { name: "Admit a workload" }),
  ).toHaveProperty("disabled", true);
});
