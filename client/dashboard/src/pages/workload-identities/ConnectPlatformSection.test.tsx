import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WorkloadOrganizationConnectionDetails } from "@gram/client/models/components/workloadorganizationconnectiondetails.js";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ConnectPlatformSection } from "./ConnectPlatformSection";

const ready: WorkloadOrganizationConnectionDetails = {
  available: true,
  issuer: "https://auth.example.com/o/acme",
  tokenEndpoint: "https://auth.example.com/o/acme/token",
  metadataUrl:
    "https://auth.example.com/.well-known/oauth-authorization-server/o/acme",
  onAuthenticationHost: true,
  grantTypesSupported: ["urn:ietf:params:oauth:grant-type:jwt-bearer"],
  workloadGrantAdvertised: true,
  ready: true,
};

const mocks = vi.hoisted(() => ({
  details: {
    data: undefined as unknown,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
}));

vi.mock(
  "@gram/client/react-query/workloadOrganizationConnectionDetails.js",
  () => ({
    useWorkloadOrganizationConnectionDetails: () => mocks.details,
  }),
);

beforeEach(() => {
  mocks.details = {
    data: ready,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  };
});
afterEach(cleanup);

function renderSection() {
  render(
    <TooltipProvider>
      <ConnectPlatformSection />
    </TooltipProvider>,
  );
}

it("shows the organization's token endpoint, issuer and audience rule", () => {
  renderSection();

  expect(screen.getByText(ready.tokenEndpoint).tagName).toBe("CODE");
  expect(screen.getByText(ready.issuer).tagName).toBe("CODE");
  expect(
    screen.getByRole("button", { name: "Copy token endpoint" }),
  ).toBeTruthy();
  expect(
    screen.getByRole("button", { name: "Copy authorization server issuer" }),
  ).toBeTruthy();
  expect(
    screen.getByText(/aud must be exactly this issuer or the token endpoint/),
  ).toBeTruthy();
  expect(screen.getByText(/allowed API hosts/)).toBeTruthy();
});

it("offers no MCP server picker", () => {
  renderSection();

  expect(screen.queryByRole("combobox")).toBeNull();
  expect(screen.queryByText("MCP server")).toBeNull();
});

it("says the endpoint isn't enabled instead of showing values", () => {
  mocks.details.data = {
    ...ready,
    available: false,
    grantTypesSupported: [],
    workloadGrantAdvertised: false,
    ready: false,
    notReadyReason: "organization_endpoint_disabled",
  };
  renderSection();

  expect(
    screen.getByText(
      "Organization token endpoint isn't enabled for this organization",
    ),
  ).toBeTruthy();
  expect(screen.queryByText(ready.tokenEndpoint)).toBeNull();
  expect(screen.queryByText(ready.issuer)).toBeNull();
});

it("keeps the values but warns when agent authorization is off", () => {
  mocks.details.data = {
    ...ready,
    ready: false,
    notReadyReason: "agent_rollout_disabled",
  };
  renderSection();

  expect(screen.getByText("Not ready: exchanges will fail")).toBeTruthy();
  expect(screen.getByText(ready.tokenEndpoint)).toBeTruthy();
});

it("says where the token endpoint lives", () => {
  mocks.details.data = { ...ready, onAuthenticationHost: false };
  renderSection();

  expect(screen.getByText(/On the platform host/)).toBeTruthy();
});

it("offers a retry when the connection details fail to load", async () => {
  const refetch = vi.fn();
  mocks.details = { data: undefined, isPending: false, isError: true, refetch };
  renderSection();

  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Try again" }));
  expect(refetch).toHaveBeenCalled();
});
