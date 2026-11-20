import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ProviderLookupError } from "./ema";
import { EnterpriseManagedAuthSection } from "./EnterpriseManagedAuthSection";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: ReactNode | ((props: { disabled: boolean }) => ReactNode);
  }) => (
    <>
      {typeof children === "function"
        ? children({ disabled: false })
        : children}
    </>
  ),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/acme/identity" } }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
const okta = {
  isPending: false,
  isError: false,
  data: { id: "remote-1", name: "Okta", issuer: "https://acme.okta.com" },
};
const trusted = vi.hoisted(() => ({
  state: undefined as unknown,
}));
vi.mock("@gram/client/react-query/organizationRemoteSessionIssuer.js", () => ({
  useOrganizationRemoteSessionIssuer: () => trusted.state,
}));
vi.mock("@/lib/remote-identity/components/ProviderLink", () => ({
  IssuerLink: ({ issuer }: { issuer: { id: string; name: string } }) => (
    <a href={`/remote-identity-providers/${issuer.id}`}>{issuer.name}</a>
  ),
}));
const ema = vi.hoisted(() => ({
  lookUpProvider: vi.fn(),
  turnOnEma: vi.fn(),
}));
vi.mock("./ema", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./ema")>()),
  lookUpProvider: ema.lookUpProvider,
  turnOnEma: ema.turnOnEma,
}));
vi.mock("./invalidateIssuers", () => ({
  invalidateIssuers: () => Promise.resolve(),
}));
vi.mock("./MakeOrganizationWideDialog", () => ({
  MakeOrganizationWideDialog: () => <div role="dialog">Make wide</div>,
}));

const base = {
  id: "usi-1",
  slug: "acme-workforce",
  projectId: "",
  organizationId: "org-1",
  issuerUrl: "https://app.example.com/oauth/usi/usi-1",
} as UserSessionIssuer;

function show(
  issuer: UserSessionIssuer,
  path = "/acme/mcp-authentication/usi-1",
) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <MemoryRouter initialEntries={[path]}>
          <EnterpriseManagedAuthSection issuer={issuer} />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  trusted.state = okta;
  ema.lookUpProvider.mockReset();
  ema.turnOnEma.mockReset();
});

function lookUp(address: string) {
  fireEvent.click(screen.getByRole("button", { name: "Set up EMA" }));
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: address },
  });
  fireEvent.click(screen.getByRole("button", { name: "Look up" }));
}
afterEach(cleanup);

describe("EnterpriseManagedAuthSection", () => {
  it("sets up with just the issuer URL, no provider choice", () => {
    show(base);
    expect(screen.getByText("Off")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Set up EMA" }));
    expect(screen.queryByRole("radio")).toBeNull();
    expect(screen.getByLabelText("Issuer URL")).toBeDefined();
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("turns EMA on with an Okta org URL", async () => {
    const found = {
      issuer: "https://acme.okta.com",
      draft: { issuer: "https://acme.okta.com" },
    };
    ema.lookUpProvider.mockResolvedValue(found);
    ema.turnOnEma.mockResolvedValue(undefined);
    show(base);
    lookUp("https://acme.okta.com");
    await screen.findByText(/acme\.okta\.com publishes its signing keys/);
    expect(ema.lookUpProvider).toHaveBeenCalledWith(
      expect.anything(),
      "https://acme.okta.com",
    );
    fireEvent.click(screen.getByRole("button", { name: "Turn on EMA" }));
    await waitFor(() => {
      expect(ema.turnOnEma).toHaveBeenCalledWith(
        expect.anything(),
        base,
        found,
      );
    });
  });

  it("shows why a lookup failed and keeps EMA off", async () => {
    ema.lookUpProvider.mockRejectedValue(
      new ProviderLookupError("This provider doesn't publish signing keys."),
    );
    show(base);
    lookUp("https://login.example.com");
    await screen.findByText("This provider doesn't publish signing keys.");
    expect(
      screen
        .getByRole("button", { name: "Turn on EMA" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  it("offers the issuer URL field again to change the provider", () => {
    show({ ...base, trustedRemoteSessionIssuerId: "remote-1" });
    fireEvent.click(screen.getByRole("button", { name: "Change provider" }));
    expect(screen.getByLabelText("Issuer URL")).toBeDefined();
    expect(screen.queryByRole("radio")).toBeNull();
  });

  it("names the trusted provider and offers to turn it off", () => {
    show({ ...base, trustedRemoteSessionIssuerId: "remote-1" });
    expect(screen.getByText("On")).toBeDefined();
    expect(screen.getByText("Okta")).toBeDefined();
    expect(screen.getByRole("button", { name: "Turn off" })).toBeDefined();
  });

  it("sends changes to the Okta connection when browser sign-in uses the provider too", () => {
    show({
      ...base,
      trustedRemoteSessionIssuerId: "remote-1",
      trustedRemoteSessionClientId: "client-1",
    });
    expect(screen.queryByRole("button", { name: "Turn off" })).toBeNull();
    expect(
      screen
        .getByRole("link", { name: "Okta connection" })
        .getAttribute("href"),
    ).toContain("view=setup");
  });

  it("points a provider other than Okta at its own page, not Okta's", () => {
    trusted.state = {
      ...okta,
      data: {
        id: "remote-1",
        name: "Login",
        issuer: "https://login.example.com",
      },
    };
    show({
      ...base,
      trustedRemoteSessionIssuerId: "remote-1",
      trustedRemoteSessionClientId: "client-1",
    });
    expect(screen.queryByRole("link", { name: "Okta connection" })).toBeNull();
    expect(
      screen.getByRole("link", { name: "Login" }).getAttribute("href"),
    ).toBe("/remote-identity-providers/remote-1");
  });

  it("treats a provider Okta's own setup created as Okta, whatever its domain", () => {
    trusted.state = {
      ...okta,
      data: {
        id: "remote-1",
        slug: "okta-sign-in",
        name: "Login",
        issuer: "https://login.example.com",
      },
    };
    show({
      ...base,
      trustedRemoteSessionIssuerId: "remote-1",
      trustedRemoteSessionClientId: "client-1",
    });
    expect(screen.getByRole("link", { name: "Okta connection" })).toBeDefined();
  });

  it("says so when the trusted provider can't be loaded", () => {
    trusted.state = { isPending: false, isError: true, data: undefined };
    show({ ...base, trustedRemoteSessionIssuerId: "remote-1" });
    expect(screen.queryByText("Loading…")).toBeNull();
    expect(
      screen.getByText(/Couldn't load this identity provider/),
    ).toBeDefined();
  });

  it("asks a project-specific provider to become organization-wide first", () => {
    show({ ...base, slug: "slack", projectId: "project-1" });
    expect(screen.queryByRole("button", { name: "Set up EMA" })).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Make organization-wide" }),
    );
    expect(screen.getByRole("dialog")).toBeDefined();
  });
});
