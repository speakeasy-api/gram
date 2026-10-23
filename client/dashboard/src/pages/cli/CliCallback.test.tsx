import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import CliCallback from "./CliCallback";

const mocks = vi.hoisted(() => ({
  createKey: vi.fn(),
  authorizeCode: vi.fn(),
  switchScopes: vi.fn(),
  refetch: vi.fn(),
  locationSetter: vi.fn(),
  locationReplace: vi.fn(),
  useSessionData: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({ useSessionData: mocks.useSessionData }));
vi.mock("@gram/client/react-query/createAPIKey", () => ({
  useCreateAPIKeyMutation: () => ({ mutateAsync: mocks.createKey }),
}));
vi.mock("@gram/client/react-query/cliAuthAuthorize", () => ({
  useCliAuthAuthorizeMutation: () => ({ mutateAsync: mocks.authorizeCode }),
}));
vi.mock("@gram/client/react-query/switchScopes", () => ({
  useSwitchScopesMutation: () => ({ mutateAsync: mocks.switchScopes }),
}));

function sessionFor(activeOrganizationId: string) {
  return {
    session: "<SESSION>",
    activeOrganizationId,
    organizations: [{ id: "<ORG_A>" }, { id: "<ORG_B>" }],
    organization: { projects: [] },
    user: { email: "" },
    refetch: mocks.refetch,
  };
}

let originalLocation: Location | undefined;

beforeEach(() => {
  mocks.locationSetter.mockReset();
  mocks.locationReplace.mockReset();
  mocks.createKey.mockReset().mockResolvedValue({ key: "test-key" });
  mocks.authorizeCode.mockReset();
  mocks.switchScopes.mockReset().mockResolvedValue(undefined);
  mocks.refetch.mockReset().mockResolvedValue(undefined);
  originalLocation = window.location;
  // @ts-expect-error test-only location replacement for redirect assertion
  delete window.location;
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      get href() {
        return "https://app.example/cli/callback?state=abc";
      },
      set href(value: string) {
        mocks.locationSetter(value);
      },
      replace: mocks.locationReplace,
    },
  });
});

afterEach(() => {
  cleanup();
  if (originalLocation) {
    Object.defineProperty(window, "location", {
      configurable: true,
      value: originalLocation,
    });
  }
  vi.clearAllMocks();
});

describe("CliCallback", () => {
  it("sends an authenticated zero-org session to sign-up with the callback destination", async () => {
    mocks.useSessionData.mockReturnValue({
      status: "success",
      session: {
        session: "<SESSION>",
        activeOrganizationId: "",
        organization: { projects: [] },
        user: { email: "" },
      },
    });

    render(
      <CliCallback
        localCallbackUrl="http://localhost:3000/callback"
        callbackMethod="get"
      />,
    );

    await waitFor(() => {
      expect(mocks.locationSetter).toHaveBeenCalledWith(
        "/sign-up?redirect=https%3A%2F%2Fapp.example%2Fcli%2Fcallback%3Fstate%3Dabc",
      );
    });

    expect(mocks.createKey).not.toHaveBeenCalled();
    expect(mocks.authorizeCode).not.toHaveBeenCalled();
  });

  it("switches a member to the requested organization and continues", async () => {
    mocks.useSessionData.mockReturnValue({
      status: "success",
      session: sessionFor("<ORG_A>"),
    });

    const { rerender } = render(
      <CliCallback
        localCallbackUrl="http://localhost:3000/callback"
        callbackMethod="get"
        keyScope="hooks"
        organizationId="<ORG_B>"
      />,
    );

    await waitFor(() => {
      expect(mocks.refetch).toHaveBeenCalled();
    });
    expect(mocks.switchScopes).toHaveBeenCalledWith({
      request: { organizationId: "<ORG_B>", gramSession: "<SESSION>" },
    });
    expect(mocks.createKey).not.toHaveBeenCalled();

    mocks.useSessionData.mockReturnValue({
      status: "success",
      session: sessionFor("<ORG_B>"),
    });
    rerender(
      <CliCallback
        localCallbackUrl="http://localhost:3000/callback"
        callbackMethod="get"
        keyScope="hooks"
        organizationId="<ORG_B>"
      />,
    );

    await waitFor(() => {
      expect(mocks.locationReplace).toHaveBeenCalledWith(
        "http://localhost:3000/callback?api_key=test-key&organization_id=%3CORG_B%3E",
      );
    });
    expect(mocks.switchScopes).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Error")).toBeNull();
  });

  it("refuses an organization the account is not a member of", async () => {
    mocks.useSessionData.mockReturnValue({
      status: "success",
      session: sessionFor("<ORG_A>"),
    });

    render(
      <CliCallback
        localCallbackUrl="http://localhost:3000/callback"
        callbackMethod="get"
        organizationId="<ORG_C>"
      />,
    );

    expect(await screen.findByText(/not a member of/)).toBeTruthy();
    expect(mocks.switchScopes).not.toHaveBeenCalled();
    expect(mocks.createKey).not.toHaveBeenCalled();
  });

  it("stops if the switch does not take effect", async () => {
    mocks.useSessionData.mockReturnValue({
      status: "success",
      session: sessionFor("<ORG_A>"),
    });

    const { rerender } = render(
      <CliCallback
        localCallbackUrl="http://localhost:3000/callback"
        callbackMethod="get"
        organizationId="<ORG_B>"
      />,
    );

    await waitFor(() => {
      expect(mocks.refetch).toHaveBeenCalled();
    });
    mocks.useSessionData.mockReturnValue({
      status: "success",
      session: sessionFor("<ORG_A>"),
    });
    rerender(
      <CliCallback
        localCallbackUrl="http://localhost:3000/callback"
        callbackMethod="get"
        organizationId="<ORG_B>"
      />,
    );

    expect(await screen.findByText(/not a member of/)).toBeTruthy();
    expect(mocks.switchScopes).toHaveBeenCalledTimes(1);
    expect(mocks.createKey).not.toHaveBeenCalled();
  });
});
