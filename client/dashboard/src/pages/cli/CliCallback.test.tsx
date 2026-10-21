import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import CliCallback from "./CliCallback";

const mocks = vi.hoisted(() => ({
  createKey: vi.fn(),
  authorizeCode: vi.fn(),
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

let originalLocation: Location | undefined;

beforeEach(() => {
  mocks.locationSetter.mockReset();
  mocks.locationReplace.mockReset();
  mocks.createKey.mockReset().mockResolvedValue({ key: "test-key" });
  mocks.authorizeCode.mockReset();
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
});
