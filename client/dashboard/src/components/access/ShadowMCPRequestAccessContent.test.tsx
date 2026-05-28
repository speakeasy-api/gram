import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { StrictMode, type ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  createApprovalRequest: vi.fn(),
  useSession: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: mocks.useSession,
}));

vi.mock("@/lib/utils", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/utils")>();
  return {
    ...actual,
    buildLoginRedirectURL: (redirectTo: string | null) =>
      `/rpc/auth.login${redirectTo ? `?redirect=${encodeURIComponent(redirectTo)}` : ""}`,
  };
});

vi.mock("@gram/client/react-query", () => ({
  useCreateShadowMCPApprovalRequestMutation: () => ({
    mutateAsync: mocks.createApprovalRequest,
  }),
}));

vi.mock("@speakeasy-api/moonshine", () => ({
  Button: Object.assign(
    ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
      <button onClick={onClick}>{children}</button>
    ),
    {
      LeftIcon: ({ children }: { children: ReactNode }) => (
        <span>{children}</span>
      ),
      Text: ({ children }: { children: ReactNode }) => <span>{children}</span>,
    },
  ),
  Icon: ({ name }: { name: string }) => <span data-icon={name} />,
  Stack: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import { ShadowMCPRequestAccessContent } from "./ShadowMCPRequestAccessContent";

function renderPage(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <ShadowMCPRequestAccessContent />
    </MemoryRouter>,
  );
}

function renderPageStrict(initialPath: string) {
  return render(
    <StrictMode>
      <MemoryRouter initialEntries={[initialPath]}>
        <ShadowMCPRequestAccessContent />
      </MemoryRouter>
    </StrictMode>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  window.history.replaceState(null, "", "/");
  mocks.createApprovalRequest.mockReset();
  mocks.createApprovalRequest.mockResolvedValue({});
  mocks.useSession.mockReturnValue({ session: "" });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("ShadowMCPRequestAccessContent", () => {
  it("captures fragment token, scrubs the URL, and redirects to login without the token", async () => {
    const replaceState = vi
      .spyOn(window.history, "replaceState")
      .mockImplementation(() => {});
    const location = window.location;
    const hrefSetter = vi.fn();
    // @ts-expect-error jsdom-compatible location replacement for redirect assertion
    delete window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: {
        ...location,
        pathname: "/shadow-mcp/request",
        hash: "#request_token=smar1.secret-token",
        set href(value: string) {
          hrefSetter(value);
        },
        get href() {
          return "https://app.example.test/shadow-mcp/request#request_token=smar1.secret-token";
        },
      },
    });

    renderPage("/shadow-mcp/request#request_token=smar1.secret-token");

    await waitFor(() => {
      expect(sessionStorage.getItem("shadowMcpApprovalRequestToken")).toBe(
        "smar1.secret-token",
      );
    });
    expect(replaceState).toHaveBeenCalledWith(null, "", "/shadow-mcp/request");
    await waitFor(() => {
      expect(hrefSetter).toHaveBeenCalledWith(
        "/rpc/auth.login?redirect=%2Fshadow-mcp%2Frequest",
      );
    });
    expect(hrefSetter.mock.calls[0]?.[0]).not.toContain("smar1.secret-token");
    expect(mocks.createApprovalRequest).not.toHaveBeenCalled();

    Object.defineProperty(window, "location", {
      configurable: true,
      value: location,
    });
  });

  it("ignores query tokens so request tokens are not exposed in referrers", () => {
    window.history.replaceState(
      null,
      "",
      "/shadow-mcp/request?request_token=smar1.query-token",
    );

    renderPage("/shadow-mcp/request?request_token=smar1.query-token");

    expect(screen.getByText("Link expired")).toBeTruthy();
    expect(sessionStorage.getItem("shadowMcpApprovalRequestToken")).toBeNull();
    expect(mocks.createApprovalRequest).not.toHaveBeenCalled();
  });

  it("submits the stored request token after authentication", async () => {
    sessionStorage.setItem(
      "shadowMcpApprovalRequestToken",
      "smar1.stored-token",
    );
    mocks.useSession.mockReturnValue({ session: "session_123" });

    renderPage("/shadow-mcp/request");

    await waitFor(() => {
      expect(mocks.createApprovalRequest).toHaveBeenCalledWith({
        request: {
          createShadowMCPApprovalRequestForm: {
            requestToken: "smar1.stored-token",
          },
        },
      });
    });
  });

  it("shows success after submitting even after clearing the stored token", async () => {
    sessionStorage.setItem(
      "shadowMcpApprovalRequestToken",
      "smar1.stored-token",
    );
    mocks.useSession.mockReturnValue({ session: "session_123" });

    renderPage("/shadow-mcp/request");

    await waitFor(() => {
      expect(mocks.createApprovalRequest).toHaveBeenCalled();
    });

    await waitFor(() => {
      expect(
        sessionStorage.getItem("shadowMcpApprovalRequestToken"),
      ).toBeNull();
      expect(screen.getByText("Request sent")).toBeTruthy();
      expect(screen.getByText("You can close this page.")).toBeTruthy();
    });
  });

  it("shows success under StrictMode without double-submitting", async () => {
    sessionStorage.setItem(
      "shadowMcpApprovalRequestToken",
      "smar1.strict-token",
    );
    mocks.useSession.mockReturnValue({ session: "session_123" });

    renderPageStrict("/shadow-mcp/request");

    await waitFor(() => {
      expect(
        sessionStorage.getItem("shadowMcpApprovalRequestToken"),
      ).toBeNull();
      expect(screen.getByText("Request sent")).toBeTruthy();
    });
    expect(mocks.createApprovalRequest).toHaveBeenCalledOnce();
  });

  it("shows submit failure separately and retries the stored token", async () => {
    sessionStorage.setItem(
      "shadowMcpApprovalRequestToken",
      "smar1.retry-token",
    );
    mocks.useSession.mockReturnValue({ session: "session_123" });
    mocks.createApprovalRequest
      .mockRejectedValueOnce(new Error("network failed"))
      .mockResolvedValueOnce({});

    renderPage("/shadow-mcp/request");

    await waitFor(() => {
      expect(screen.getByText("Request failed")).toBeTruthy();
    });
    expect(
      screen.getByText(
        "We could not send this request. Check your connection and try again.",
      ),
    ).toBeTruthy();
    expect(sessionStorage.getItem("shadowMcpApprovalRequestToken")).toBe(
      "smar1.retry-token",
    );

    fireEvent.click(screen.getByText("Try again"));

    await waitFor(() => {
      expect(screen.getByText("Request sent")).toBeTruthy();
    });
    expect(mocks.createApprovalRequest).toHaveBeenCalledTimes(2);
    expect(sessionStorage.getItem("shadowMcpApprovalRequestToken")).toBeNull();
  });
});
