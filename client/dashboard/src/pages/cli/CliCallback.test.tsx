import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import CliCallback from "./CliCallback";

const mocks = vi.hoisted(() => ({
  createKey: vi.fn(),
  authorizeCode: vi.fn(),
  locationSetter: vi.fn(),
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
      replace: vi.fn(),
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
  });
});
