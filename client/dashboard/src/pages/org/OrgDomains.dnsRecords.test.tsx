import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { CustomDomain } from "@gram/client/models/components/customdomain.js";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import OrgDomains from "./OrgDomains";
import { TooltipProvider } from "@/components/ui/Tooltip";

type MutateOptions = {
  onSuccess?: (result: CustomDomain) => void;
  onError?: (error: Error) => void;
};

// registerResult decides how a registerDomain call settles: with a domain, an
// error, or "deferred" to leave it pending for the test to settle later.
const state = vi.hoisted(() => ({
  domain: undefined as CustomDomain | undefined,
  registerResult: undefined as CustomDomain | Error | "deferred" | undefined,
  registerCalls: [] as unknown[],
  pendingRegister: undefined as MutateOptions | undefined,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org_example" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/hooks/useNetworkIngressRollout", () => ({
  useNetworkIngressRollout: () => ({
    canManageIngress: false,
    status: "disabled",
    rolloutEnabled: false,
  }),
}));
vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => "enterprise",
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  customDomainMcpEndpointUrl: (domain: string, slug: string) =>
    `https://${domain}/mcp/${slug}`,
  useCustomDomain: () => ({
    domain: state.domain,
    dnsConfig: { cnameTarget: "cname.example.net.", aRecords: [] },
    isLoading: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@/hooks/useRootMcpEndpoint", () => ({
  useRootMcpEndpointMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/rootMcpServers", () => ({
  useRootMcpServers: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/customDomainMcpEndpoints", () => ({
  useCustomDomainMcpEndpoints: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/registerDomain", () => ({
  useRegisterDomainMutation: () => ({
    isPending: false,
    mutate: (request: unknown, options?: MutateOptions) => {
      state.registerCalls.push(request);
      const result = state.registerResult;
      if (result === "deferred") state.pendingRegister = options;
      else if (result instanceof Error) options?.onError?.(result);
      else if (result) options?.onSuccess?.(result);
    },
  }),
}));
vi.mock("@gram/client/react-query/deleteDomain", () => ({
  useDeleteDomainMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/updateDomain", () => ({
  useUpdateDomainMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/checkDomainHealth", () => ({
  useCheckDomainHealthMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/components/page-templates", async (importOriginal) => {
  const original = await importOriginal<object>();
  return {
    ...original,
    // The real page chrome needs a session and router context; the dialogs
    // and domain card are what these tests exercise.
    SettingsPage: ({ children }: { children: ReactNode }) => (
      <main>{children}</main>
    ),
  };
});
vi.mock("./PrivateNetworkSection", () => ({
  PrivateNetworkSection: () => null,
}));

const target = "abcdefgh23.domains.example.net";

function customDomain(overrides: Partial<CustomDomain> = {}): CustomDomain {
  return {
    id: "6a6c0a36-8f7a-4e3c-9b2f-3d1e4c5b6a7d",
    organizationId: "org_example",
    domain: "mcp.example.com",
    verified: false,
    activated: false,
    isUpdating: true,
    ipAllowlist: [],
    suggestedRecordType: "cname",
    createdAt: new Date("2026-11-01T00:00:00Z"),
    updatedAt: new Date("2026-11-01T00:00:00Z"),
    dnsTargetToken: "abcdefgh23",
    dnsTarget: target,
    ...overrides,
  };
}

function renderPage() {
  render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <OrgDomains />
        </TooltipProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  state.domain = undefined;
  state.registerResult = undefined;
  state.registerCalls = [];
  state.pendingRegister = undefined;
});

function submitRegistration(domain: string) {
  fireEvent.click(screen.getByRole("button", { name: "Add Domain" }));
  fireEvent.change(screen.getByPlaceholderText(/Enter your domain/), {
    target: { value: domain },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Register and continue" }),
  );
}
afterEach(cleanup);

it("registers the domain first, then shows its designated CNAME target in the same dialog", () => {
  state.registerResult = customDomain();
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "Add Domain" }));
  expect(screen.queryByText(target)).toBeNull();
  fireEvent.change(screen.getByPlaceholderText(/Enter your domain/), {
    target: { value: "mcp.example.com" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Register and continue" }),
  );

  expect(state.registerCalls).toHaveLength(1);
  expect(screen.getByRole("heading", { name: "Add DNS records" })).toBeTruthy();
  expect(screen.getByText(target)).toBeTruthy();
  expect(screen.getByText("_gram.mcp.example.com")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Check now" })).toBeTruthy();
});

it("reopens the DNS records from the domain card while verification is pending", () => {
  state.domain = customDomain();
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "View DNS records" }));

  expect(screen.getByRole("heading", { name: "Add DNS records" })).toBeTruthy();
  expect(screen.getByText(target)).toBeTruthy();
});

it("shows the shared CNAME when per-domain targets are not configured", () => {
  state.domain = customDomain({ dnsTarget: undefined });
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "View DNS records" }));

  expect(screen.getByText("cname.example.net.")).toBeTruthy();
});

it("recommends the designated target to a working shared CNAME domain", () => {
  state.domain = customDomain({
    verified: true,
    activated: true,
    isUpdating: false,
    healthStatus: "healthy",
    dnsRoutingMethod: "shared_cname",
  });
  renderPage();

  expect(screen.getByText("Recommended update")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Check again" })).toBeTruthy();
});

it("drops the recommendation once the domain uses its designated target", () => {
  state.domain = customDomain({
    verified: true,
    activated: true,
    isUpdating: false,
    healthStatus: "healthy",
    dnsRoutingMethod: "per_domain_cname",
  });
  renderPage();

  expect(screen.queryByText("Recommended update")).toBeNull();
});

it("keeps the register step and shows the error when registration fails", () => {
  state.registerResult = new Error("domain is already registered");
  renderPage();

  submitRegistration("mcp.example.com");

  expect(
    screen.getByRole("heading", { name: "Connect a Custom Domain" }),
  ).toBeTruthy();
  expect(screen.getByText("domain is already registered")).toBeTruthy();
  expect(screen.queryByRole("heading", { name: "Add DNS records" })).toBeNull();
});

it("checks DNS for the pending domain from the records dialog and closes it", () => {
  state.domain = customDomain();
  state.registerResult = customDomain();
  renderPage();

  fireEvent.click(screen.getByRole("button", { name: "View DNS records" }));
  const dialog = screen.getByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Check now" }));

  expect(state.registerCalls).toEqual([
    {
      security: { sessionHeaderGramSession: "" },
      request: { createDomainRequestBody: { domain: "mcp.example.com" } },
    },
  ]);
  expect(screen.queryByRole("dialog")).toBeNull();
});

it("starts a fresh registration after the records dialog closes", () => {
  state.registerResult = customDomain();
  renderPage();

  submitRegistration("mcp.example.com");
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  fireEvent.click(screen.getByRole("button", { name: "Add Domain" }));

  expect(
    screen.getByRole("heading", { name: "Connect a Custom Domain" }),
  ).toBeTruthy();
  expect(
    (screen.getByPlaceholderText(/Enter your domain/) as HTMLInputElement)
      .value,
  ).toBe("");
});

it("does not reopen the dialog when registration finishes after it was dismissed", () => {
  state.registerResult = "deferred";
  renderPage();

  submitRegistration("mcp.example.com");
  fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
  expect(screen.queryByRole("dialog")).toBeNull();

  act(() => state.pendingRegister?.onSuccess?.(customDomain()));

  expect(screen.queryByRole("dialog")).toBeNull();
});

it("shows one recheck action and separates fresh DNS from infrastructure", () => {
  state.domain = customDomain({
    verified: true,
    activated: true,
    isUpdating: false,
    dnsRoutingMethod: "shared_cname",
    dnsConfigurationStatus: "matched",
    dnsCheckedAt: new Date("2026-11-01T00:00:00Z"),
    healthCheckedAt: new Date("2026-11-01T00:00:00Z"),
    healthStatus: "healthy",
  });
  renderPage();
  expect(screen.getAllByRole("button", { name: "Check again" })).toHaveLength(
    1,
  );
  expect(screen.getByText(/Infrastructure: Ready/)).toBeTruthy();
});

it("does not treat an older worker's later health write as fresh infrastructure", () => {
  state.domain = customDomain({
    verified: true,
    activated: true,
    isUpdating: false,
    dnsConfigurationStatus: "matched",
    dnsRoutingMethod: "per_domain_cname",
    dnsCheckedAt: new Date("2026-11-01T00:00:00Z"),
    healthCheckedAt: new Date("2026-11-01T01:00:00Z"),
    healthStatus: "unhealthy",
    healthIssue: "check_failed",
    consecutiveFailures: 2,
  });
  renderPage();
  expect(
    screen.getByText(/Infrastructure: not checked separately yet/),
  ).toBeTruthy();
  expect(screen.getByText("Review the previous health check")).toBeTruthy();
  expect(screen.getAllByRole("button", { name: "Check again" })).toHaveLength(
    1,
  );
});

it("shows the designated target when DNS points at another domain's token", () => {
  state.domain = customDomain({
    verified: true,
    activated: true,
    isUpdating: false,
    dnsConfigurationStatus: "wrong_target",
    dnsRoutingMethod: "unknown",
  });
  renderPage();
  expect(screen.getByText(/point to another domain/)).toBeTruthy();
  expect(screen.getByText(`Designated target: ${target}`)).toBeTruthy();
});

it("shows saved DNS guidance before ownership is verified", () => {
  state.domain = customDomain({
    verified: false,
    dnsConfigurationStatus: "not_found",
  });
  renderPage();
  expect(screen.getByText("DNS configuration")).toBeTruthy();
  expect(screen.getByText(/The domain name was not found in DNS/)).toBeTruthy();
});

it("keeps shared-target DNS observations visible without a designated target", () => {
  state.domain = customDomain({
    verified: true,
    dnsTarget: undefined,
    dnsConfigurationStatus: "unconfirmed",
  });
  renderPage();
  expect(
    screen.getByText(/The routing target could not be confirmed/),
  ).toBeTruthy();
});

it.each([undefined, "not_configured"])(
  "hides unavailable DNS diagnostics (%s) without a designated target",
  (dnsConfigurationStatus) => {
    state.domain = customDomain({
      verified: true,
      dnsTarget: undefined,
      dnsConfigurationStatus,
    });
    renderPage();
    expect(screen.queryByText("DNS configuration")).toBeNull();
  },
);

it("keeps the infrastructure recheck available without DNS target configuration", () => {
  state.domain = customDomain({
    verified: true,
    activated: true,
    dnsTarget: undefined,
    dnsConfigurationStatus: "not_configured",
  });
  renderPage();
  expect(screen.getByRole("button", { name: "Check again" })).toBeTruthy();
});

it("distinguishes an unavailable DNS check from a check that has never run", () => {
  state.domain = customDomain({
    verified: true,
    dnsConfigurationStatus: "not_configured",
    dnsCheckedAt: new Date("2026-11-01T00:00:00Z"),
  });
  renderPage();
  expect(
    screen.getByText("DNS target verification is unavailable."),
  ).toBeTruthy();
  expect(
    screen.queryByText("DNS configuration has not been checked yet."),
  ).toBeNull();
});
