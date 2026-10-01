import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WorkloadOrganizationConnectionDetails } from "@gram/client/models/components/workloadorganizationconnectiondetails.js";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { OrganizationTokenEndpoint } from "./OrganizationTokenEndpoint";

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

function renderEndpoint() {
  render(
    <TooltipProvider>
      <OrganizationTokenEndpoint />
    </TooltipProvider>,
  );
}

it("shows the organization's token endpoint with a copy button and the resource hint", () => {
  renderEndpoint();

  expect(screen.getByRole("group", { name: "Token endpoint" })).toBeTruthy();
  expect(screen.getByText(ready.tokenEndpoint).tagName).toBe("CODE");
  expect(
    screen.getByRole("button", { name: "Copy token endpoint" }),
  ).toBeTruthy();
  expect(
    screen.getByText(/names the MCP server it wants as resource/),
  ).toBeTruthy();
});

it("offers no MCP server picker", () => {
  renderEndpoint();

  expect(screen.queryByRole("combobox")).toBeNull();
});

it("says the endpoint isn't enabled instead of showing a URL", () => {
  mocks.details.data = {
    ...ready,
    available: false,
    grantTypesSupported: [],
    workloadGrantAdvertised: false,
    ready: false,
    notReadyReason: "organization_endpoint_disabled",
  };
  renderEndpoint();

  expect(
    screen.getByText(
      "Organization token endpoint isn't enabled for this organization",
    ),
  ).toBeTruthy();
  expect(screen.queryByText(ready.tokenEndpoint)).toBeNull();
  expect(
    screen.queryByRole("button", { name: "Copy token endpoint" }),
  ).toBeNull();
  expect(screen.queryByText(/names the MCP server it wants/)).toBeNull();
});

it("says the deployment accepts no workload tokens when the grant is unavailable", () => {
  mocks.details.data = {
    ...ready,
    grantTypesSupported: [],
    workloadGrantAdvertised: false,
    ready: false,
    notReadyReason: "workload_grant_unavailable",
  };
  renderEndpoint();

  expect(
    screen.getByText("This deployment doesn't accept workload identity tokens"),
  ).toBeTruthy();
  expect(screen.queryByText(ready.tokenEndpoint)).toBeNull();
});

it("keeps the endpoint but warns when agent authorization is off", () => {
  mocks.details.data = {
    ...ready,
    ready: false,
    notReadyReason: "agent_rollout_disabled",
  };
  renderEndpoint();

  expect(screen.getByText(ready.tokenEndpoint)).toBeTruthy();
  expect(screen.getByText("Agent authorization is off")).toBeTruthy();
});

it("offers a retry when the endpoint fails to load", async () => {
  const refetch = vi.fn();
  mocks.details = { data: undefined, isPending: false, isError: true, refetch };
  renderEndpoint();

  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Try again" }));
  expect(refetch).toHaveBeenCalled();
});

// The register sheet mounts the endpoint inside its form, so none of its
// buttons may submit that form.
it("never submits a surrounding form", async () => {
  const onSubmit = vi.fn();
  const user = userEvent.setup();
  const { rerender } = render(
    <TooltipProvider>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <OrganizationTokenEndpoint />
      </form>
    </TooltipProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Copy token endpoint" }));

  mocks.details = {
    data: undefined,
    isPending: false,
    isError: true,
    refetch: vi.fn(),
  };
  rerender(
    <TooltipProvider>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <OrganizationTokenEndpoint />
      </form>
    </TooltipProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Try again" }));

  expect(onSubmit).not.toHaveBeenCalled();
});
