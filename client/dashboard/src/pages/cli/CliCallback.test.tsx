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

/** Signs in a member of <ORG_A> and <ORG_B> with activeOrganizationId active. */
function mockMemberSession(activeOrganizationId: string) {
  mocks.useSessionData.mockReturnValue({
    status: "success",
    session: {
      session: "<SESSION>",
      activeOrganizationId,
      organizations: [{ id: "<ORG_A>" }, { id: "<ORG_B>" }],
      organization: { projects: [] },
      user: { email: "" },
      refetch: mocks.refetch,
    },
  });
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

  it("keeps the CLI's state token on the callback URL when returning a key by redirect", async () => {
    mocks.useSessionData.mockReturnValue({
      status: "success",
      session: {
        session: "<SESSION>",
        activeOrganizationId: "<ORG_ID>",
        organization: { projects: [{ slug: "default" }] },
        user: { email: "" },
      },
    });

    render(
      <CliCallback
        localCallbackUrl="http://127.0.0.1:5555/callback?state=cli-state-token"
        projectSlug="default"
        callbackMethod="get"
      />,
    );

    await waitFor(() => {
      expect(mocks.locationReplace).toHaveBeenCalledTimes(1);
    });

    const target = new URL(mocks.locationReplace.mock.calls[0]![0] as string);
    expect(target.origin).toBe("http://127.0.0.1:5555");
    expect(target.pathname).toBe("/callback");
    expect(target.searchParams.get("state")).toBe("cli-state-token");
    expect(target.searchParams.get("api_key")).toBe("test-key");
    expect(target.searchParams.get("project")).toBe("default");
  });

  const linkForOrgB = {
    localCallbackUrl: "http://localhost:3000/callback",
    callbackMethod: "get",
    keyScope: "hooks",
    organizationId: "<ORG_B>",
  } as const;

  /**
   * Renders the <ORG_B> link while <ORG_A> is active. The session refetch after
   * the switch reports refetchedOrganizationId as active.
   */
  async function renderAndAwaitSwitch(refetchedOrganizationId: string) {
    mocks.refetch.mockResolvedValue({
      activeOrganizationId: refetchedOrganizationId,
    });
    mockMemberSession("<ORG_A>");
    const view = render(<CliCallback {...linkForOrgB} />);
    await waitFor(() => {
      expect(mocks.refetch).toHaveBeenCalled();
    });
    return view;
  }

  it("switches a member to the requested organization and continues", async () => {
    const { rerender } = await renderAndAwaitSwitch("<ORG_B>");
    expect(mocks.switchScopes).toHaveBeenCalledWith({
      request: { organizationId: "<ORG_B>", gramSession: "<SESSION>" },
    });
    expect(mocks.createKey).not.toHaveBeenCalled();

    mockMemberSession("<ORG_B>");
    rerender(<CliCallback {...linkForOrgB} />);

    await waitFor(() => {
      expect(mocks.locationReplace).toHaveBeenCalledWith(
        "http://localhost:3000/callback?api_key=test-key&organization_id=%3CORG_B%3E",
      );
    });
    expect(mocks.switchScopes).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Error")).toBeNull();
  });

  it("waits for an in-flight switch instead of failing", async () => {
    let finishSwitch = (): void => {};
    mocks.switchScopes.mockReturnValue(
      new Promise<void>((resolve) => {
        finishSwitch = resolve;
      }),
    );
    mocks.refetch.mockResolvedValue({ activeOrganizationId: "<ORG_B>" });
    mockMemberSession("<ORG_A>");
    const { rerender } = render(<CliCallback {...linkForOrgB} />);
    await waitFor(() => {
      expect(mocks.switchScopes).toHaveBeenCalled();
    });

    // A render while the switch is pending still carries the old session.
    mockMemberSession("<ORG_A>");
    rerender(<CliCallback {...linkForOrgB} />);
    finishSwitch();
    await waitFor(() => {
      expect(mocks.refetch).toHaveBeenCalled();
    });

    expect(screen.queryByText("Error")).toBeNull();
    expect(mocks.switchScopes).toHaveBeenCalledTimes(1);
  });

  it("refuses an organization the account is not a member of", async () => {
    mockMemberSession("<ORG_A>");
    render(<CliCallback {...linkForOrgB} organizationId="<ORG_C>" />);

    expect(await screen.findByText(/not a member of/)).toBeTruthy();
    expect(mocks.switchScopes).not.toHaveBeenCalled();
    expect(mocks.createKey).not.toHaveBeenCalled();
  });

  it("stops if the switch does not take effect", async () => {
    await renderAndAwaitSwitch("<ORG_A>");

    expect(await screen.findByText(/Could not switch/)).toBeTruthy();
    expect(mocks.switchScopes).toHaveBeenCalledTimes(1);
    expect(mocks.createKey).not.toHaveBeenCalled();
  });
  it("requests a producer code for the CLI and returns only the code, keeping its state", async () => {
    mockMemberSession("<ORG_A>");
    mocks.authorizeCode.mockResolvedValue({ code: "one-time-code" });

    render(
      <CliCallback
        localCallbackUrl="http://127.0.0.1:5555/callback?state=cli-state-token"
        codeChallenge="<CHALLENGE>"
        codeChallengeMethod="S256"
        pkceKeyScope="producer"
        callbackMethod="get"
      />,
    );

    await waitFor(() => {
      expect(mocks.locationReplace).toHaveBeenCalledTimes(1);
    });

    expect(mocks.authorizeCode).toHaveBeenCalledWith({
      request: {
        gramSession: "<SESSION>",
        authorizeRequestBody: {
          codeChallenge: "<CHALLENGE>",
          codeChallengeMethod: "S256",
          keyScope: "producer",
          projectSlug: undefined,
        },
      },
    });
    expect(mocks.createKey).not.toHaveBeenCalled();

    const target = new URL(mocks.locationReplace.mock.calls[0]![0] as string);
    expect(target.searchParams.get("state")).toBe("cli-state-token");
    expect(target.searchParams.get("code")).toBe("one-time-code");
    expect(target.searchParams.get("api_key")).toBeNull();
  });

  it("leaves a device-agent PKCE code at the server's default key scope", async () => {
    mockMemberSession("<ORG_A>");
    mocks.authorizeCode.mockResolvedValue({ code: "one-time-code" });

    render(
      <CliCallback
        localCallbackUrl="http://127.0.0.1:5555/callback"
        codeChallenge="<CHALLENGE>"
        codeChallengeMethod="S256"
        callbackMethod="get"
      />,
    );

    await waitFor(() => {
      expect(mocks.authorizeCode).toHaveBeenCalledTimes(1);
    });
    expect(
      mocks.authorizeCode.mock.calls[0]![0].request.authorizeRequestBody
        .keyScope,
    ).toBeUndefined();
  });
});
