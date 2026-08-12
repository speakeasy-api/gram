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

vi.mock("@gram/client/react-query/riskCreatePolicyBypassRequest.js", () => ({
  useRiskCreatePolicyBypassRequestMutation: () => ({
    mutateAsync: mocks.createApprovalRequest,
  }),
}));

vi.mock("@/components/ui/Button", () => ({
  Button: Object.assign(
    ({
      children,
      onClick,
      disabled,
    }: {
      children: ReactNode;
      onClick?: () => void;
      disabled?: boolean;
    }) => (
      <button onClick={onClick} disabled={disabled}>
        {children}
      </button>
    ),
    {
      LeftIcon: ({ children }: { children: ReactNode }) => (
        <span>{children}</span>
      ),
      Text: ({ children }: { children: ReactNode }) => <span>{children}</span>,
    },
  ),
}));

vi.mock("@/components/ui/Icon", () => ({
  Icon: ({ name }: { name: string }) => <span data-icon={name} />,
}));

vi.mock("@/components/ui/Stack", () => ({
  Stack: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@/components/ui/Textarea", () => ({
  TextArea: ({
    value,
    onChange,
    placeholder,
  }: {
    value: string;
    onChange: (value: string) => void;
    placeholder?: string;
  }) => (
    <textarea
      aria-label="note"
      placeholder={placeholder}
      value={value}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}));

import { ShadowMCPRequestAccessContent } from "./ShadowMCPRequestAccessContent";

function renderPage(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <ShadowMCPRequestAccessContent />
    </MemoryRouter>,
  );
}

// The page asks for a justification before it will send anything, so every
// submit-path test types one and clicks.
function sendWithNote(note = "Blocked from the docs workflow.") {
  fireEvent.change(screen.getByLabelText("note"), { target: { value: note } });
  fireEvent.click(screen.getByText("Send request"));
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
        // oxlint-disable-next-line typescript/no-misused-spread -- jsdom Location is plain enough for tests
        ...location,
        pathname: "/risk-policy-bypass/request",
        hash: "#request_token=rpbr1.secret-token",
        set href(value: string) {
          hrefSetter(value);
        },
        get href() {
          return "https://app.example.test/risk-policy-bypass/request#request_token=rpbr1.secret-token";
        },
      },
    });

    renderPage("/risk-policy-bypass/request#request_token=rpbr1.secret-token");

    await waitFor(() => {
      expect(sessionStorage.getItem("riskPolicyBypassRequestToken")).toBe(
        "rpbr1.secret-token",
      );
    });
    expect(replaceState).toHaveBeenCalledWith(
      null,
      "",
      "/risk-policy-bypass/request",
    );
    await waitFor(() => {
      expect(hrefSetter).toHaveBeenCalledWith(
        "/rpc/auth.login?redirect=%2Frisk-policy-bypass%2Frequest",
      );
    });
    expect(hrefSetter.mock.calls[0]?.[0]).not.toContain("rpbr1.secret-token");
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
      "/risk-policy-bypass/request?request_token=rpbr1.query-token",
    );

    renderPage("/risk-policy-bypass/request?request_token=rpbr1.query-token");

    expect(screen.getByText("Link expired")).toBeTruthy();
    expect(sessionStorage.getItem("riskPolicyBypassRequestToken")).toBeNull();
    expect(mocks.createApprovalRequest).not.toHaveBeenCalled();
  });

  it("asks why before sending, and carries the answer with the token", async () => {
    sessionStorage.setItem(
      "riskPolicyBypassRequestToken",
      "rpbr1.stored-token",
    );
    mocks.useSession.mockReturnValue({ session: "session_123" });

    renderPage("/risk-policy-bypass/request");

    // Arriving on the page must not file anything: the note is the point.
    expect(mocks.createApprovalRequest).not.toHaveBeenCalled();
    expect(screen.getByText("Request access")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("note"), {
      target: { value: "  Docs team lives in Notion.  " },
    });
    fireEvent.click(screen.getByText("Send request"));

    await waitFor(() => {
      expect(mocks.createApprovalRequest).toHaveBeenCalledWith({
        request: {
          createRiskPolicyBypassRequestRequestBody: {
            requestToken: "rpbr1.stored-token",
            note: "Docs team lives in Notion.",
          },
        },
      });
    });
  });

  it("will not send an empty justification", async () => {
    sessionStorage.setItem("riskPolicyBypassRequestToken", "rpbr1.empty-note");
    mocks.useSession.mockReturnValue({ session: "session_123" });

    renderPage("/risk-policy-bypass/request");

    const send = screen.getByText("Send request").closest("button");
    expect(send?.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText("note"), {
      target: { value: "   " },
    });
    expect(screen.getByText("Send request").closest("button")?.disabled).toBe(
      true,
    );
    expect(mocks.createApprovalRequest).not.toHaveBeenCalled();
  });

  it("shows success after submitting even after clearing the stored token", async () => {
    sessionStorage.setItem(
      "riskPolicyBypassRequestToken",
      "rpbr1.stored-token",
    );
    mocks.useSession.mockReturnValue({ session: "session_123" });

    renderPage("/risk-policy-bypass/request");
    sendWithNote();

    await waitFor(() => {
      expect(mocks.createApprovalRequest).toHaveBeenCalled();
    });

    await waitFor(() => {
      expect(sessionStorage.getItem("riskPolicyBypassRequestToken")).toBeNull();
      expect(screen.getByText("Request sent")).toBeTruthy();
      expect(screen.getByText("You can close this page.")).toBeTruthy();
    });
  });

  it("shows success under StrictMode without double-submitting", async () => {
    sessionStorage.setItem(
      "riskPolicyBypassRequestToken",
      "rpbr1.strict-token",
    );
    mocks.useSession.mockReturnValue({ session: "session_123" });

    renderPageStrict("/risk-policy-bypass/request");
    sendWithNote();

    await waitFor(() => {
      expect(sessionStorage.getItem("riskPolicyBypassRequestToken")).toBeNull();
      expect(screen.getByText("Request sent")).toBeTruthy();
    });
    expect(mocks.createApprovalRequest).toHaveBeenCalledOnce();
  });

  it("shows submit failure separately and retries the stored token", async () => {
    sessionStorage.setItem("riskPolicyBypassRequestToken", "rpbr1.retry-token");
    mocks.useSession.mockReturnValue({ session: "session_123" });
    mocks.createApprovalRequest
      .mockRejectedValueOnce(new Error("network failed"))
      .mockResolvedValueOnce({});

    renderPage("/risk-policy-bypass/request");
    sendWithNote();

    await waitFor(() => {
      expect(screen.getByText("Request failed")).toBeTruthy();
    });
    expect(
      screen.getByText(
        "We could not send this request. Check your connection and try again.",
      ),
    ).toBeTruthy();
    expect(sessionStorage.getItem("riskPolicyBypassRequestToken")).toBe(
      "rpbr1.retry-token",
    );

    fireEvent.click(screen.getByText("Try again"));

    // The typed note survives the failure, so retrying is one click.
    expect(screen.getByLabelText("note")).toHaveProperty(
      "value",
      "Blocked from the docs workflow.",
    );
    fireEvent.click(screen.getByText("Send request"));

    await waitFor(() => {
      expect(screen.getByText("Request sent")).toBeTruthy();
    });
    expect(mocks.createApprovalRequest).toHaveBeenCalledTimes(2);
    expect(sessionStorage.getItem("riskPolicyBypassRequestToken")).toBeNull();
  });
});
