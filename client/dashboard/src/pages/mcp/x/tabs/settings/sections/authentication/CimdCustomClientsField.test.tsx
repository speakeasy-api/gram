import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CimdCustomClientsField } from "./CimdCustomClientsField";

const testState = vi.hoisted(() => ({
  hasScope: true,
  items: [] as { id: string; clientIdMetadataUri: string }[],
  isLoading: false,
  isError: false,
  hasNextPage: false,
  isFetchNextPageError: false,
  fetchNextPage: vi.fn(),
  createMutate: vi.fn(),
  createPending: false,
  verifyPending: false,
  createOptions: undefined as
    | {
        onSuccess?: (result: { client: { id: string } }) => Promise<void>;
        onError?: (error: Error) => void;
      }
    | undefined,
  deleteMutate: vi.fn(),
  verifyMutate: vi.fn(),
  verifyOptions: undefined as
    | {
        onSuccess?: (result: {
          verified: boolean;
          outcome: string;
          detail: string;
          clientName?: string;
        }) => void;
        onError?: (error: Error) => void;
      }
    | undefined,
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

// Mirrors the real RequireScope contract: it disables (rather than hides)
// component-level children and supports the render-function form.
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: ReactNode | ((props: { disabled: boolean }) => ReactNode);
  }) => (
    <>
      {typeof children === "function"
        ? children({ disabled: !testState.hasScope })
        : children}
    </>
  ),
}));

vi.mock("@gram/client/react-query/userSessionIssuerCimdClients.js", () => ({
  invalidateAllUserSessionIssuerCimdClients: vi.fn(),
  useUserSessionIssuerCimdClientsInfinite: () => ({
    data: { pages: [{ result: { items: testState.items } }] },
    isLoading: testState.isLoading,
    isError: testState.isError,
    hasNextPage: testState.hasNextPage,
    isFetchingNextPage: false,
    isFetchNextPageError: testState.isFetchNextPageError,
    fetchNextPage: testState.fetchNextPage,
  }),
}));

vi.mock(
  "@gram/client/react-query/createUserSessionIssuerCimdClient.js",
  () => ({
    useCreateUserSessionIssuerCimdClientMutation: (options: {
      onSuccess?: (result: { client: { id: string } }) => Promise<void>;
      onError?: (error: Error) => void;
    }) => {
      testState.createOptions = options;
      return {
        mutate: testState.createMutate,
        isPending: testState.createPending,
      };
    },
  }),
);

vi.mock(
  "@gram/client/react-query/deleteUserSessionIssuerCimdClient.js",
  () => ({
    useDeleteUserSessionIssuerCimdClientMutation: () => ({
      mutate: testState.deleteMutate,
      isPending: false,
      variables: undefined,
    }),
  }),
);

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock(
  "@gram/client/react-query/verifyUserSessionIssuerCimdClientURL.js",
  () => ({
    useVerifyUserSessionIssuerCimdClientURLMutation: (options: {
      onSuccess?: (result: {
        verified: boolean;
        outcome: string;
        detail: string;
        clientName?: string;
      }) => void;
      onError?: (error: Error) => void;
    }) => {
      testState.verifyOptions = options;
      return {
        mutate: testState.verifyMutate,
        isPending: testState.verifyPending,
      };
    },
  }),
);

vi.mock("sonner", () => ({
  toast: {
    error: (...args: unknown[]) => testState.toastError(...args),
    success: (...args: unknown[]) => testState.toastSuccess(...args),
    warning: vi.fn(),
  },
}));

const issuer: UserSessionIssuer = {
  authnChallengeMode: "chain",
  clientIdMetadataAdmissionMode: "presets",
  createdAt: new Date(0),
  id: "issuer-1",
  projectId: "project-1",
  sessionDurationHours: 24,
  slug: "issuer",
  updatedAt: new Date(0),
};

beforeEach(() => {
  testState.hasScope = true;
  testState.isLoading = false;
  testState.isError = false;
  testState.hasNextPage = false;
  testState.isFetchNextPageError = false;
  testState.createPending = false;
  testState.verifyPending = false;
  testState.items = [];
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("CimdCustomClientsField", () => {
  it("renders the configured URLs from the API", () => {
    testState.items = [
      { id: "row-1", clientIdMetadataUri: "https://a.example.com/client.json" },
      { id: "row-2", clientIdMetadataUri: "https://b.example.com/client.json" },
    ];

    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    expect(screen.getByText("https://a.example.com/client.json")).toBeDefined();
    expect(screen.getByText("https://b.example.com/client.json")).toBeDefined();
  });

  it("explains the empty state", () => {
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    expect(screen.getByText(/No custom client URLs/)).toBeDefined();
  });

  it("adds a trimmed URL", () => {
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "  https://new.example.com/client.json  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(testState.createMutate).toHaveBeenCalledWith({
      request: {
        createUserSessionIssuerCimdClientForm: {
          userSessionIssuerId: "issuer-1",
          clientIdMetadataUri: "https://new.example.com/client.json",
        },
      },
    });
  });

  it("blocks a duplicate URL inline without calling the idempotent create", () => {
    testState.items = [
      {
        id: "row-1",
        clientIdMetadataUri: "https://dupe.example.com/client.json",
      },
    ];

    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "https://dupe.example.com/client.json" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(testState.createMutate).not.toHaveBeenCalled();
    expect(
      screen.getByText("This URL is already allowed on this issuer."),
    ).toBeDefined();
  });

  it("surfaces a server-side syntax rejection inline", () => {
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    act(() => {
      testState.createOptions?.onError?.(
        new Error("invalid client_id_metadata_uri: scheme must be https"),
      );
    });

    expect(
      screen.getByText("invalid client_id_metadata_uri: scheme must be https"),
    ).toBeDefined();
  });

  it("clears the input and confirms on a successful add", async () => {
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "https://new.example.com/client.json" },
    });

    await act(async () => {
      await testState.createOptions?.onSuccess?.({ client: { id: "row-1" } });
    });

    expect(testState.toastSuccess).toHaveBeenCalledWith("Client URL allowed");
    expect(screen.getByRole("textbox")).toHaveProperty("value", "");
  });

  it("reports a failed list fetch instead of claiming there are no URLs", () => {
    testState.isError = true;

    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    expect(
      screen.getByText(/Could not load the custom client URLs/),
    ).toBeDefined();
    expect(screen.queryByText(/No custom client URLs/)).toBeNull();
  });

  it("drains the remaining pages while more are outstanding", () => {
    testState.hasNextPage = true;

    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    expect(testState.fetchNextPage).toHaveBeenCalled();
    expect(screen.getByText(/Loading custom client URLs/)).toBeDefined();
  });

  it("stops draining and reports an error when a later page fails", () => {
    testState.hasNextPage = true;
    testState.isFetchNextPageError = true;
    testState.items = [
      { id: "row-1", clientIdMetadataUri: "https://a.example.com/client.json" },
    ];

    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    // hasNextPage stays true after a failure, so without the error guard this
    // retries forever and the list claims to be loading indefinitely.
    expect(testState.fetchNextPage).not.toHaveBeenCalled();
    expect(screen.queryByText(/Loading custom client URLs/)).toBeNull();
    expect(
      screen.getByText(/Could not load the custom client URLs/),
    ).toBeDefined();
  });

  it("toasts a refused verify request rather than blaming the field", () => {
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    act(() => {
      testState.verifyOptions?.onError?.(
        new Error("verify rate limit exceeded, try again shortly"),
      );
    });

    // Rate limit, authorization, and transport failures are outcomes of the
    // Verify action, like a refused probe, so they surface the same way.
    expect(testState.toastError).toHaveBeenCalledWith(
      "verify rate limit exceeded, try again shortly",
    );
    expect(
      screen.queryByText("verify rate limit exceeded, try again shortly"),
    ).toBeNull();
  });

  it("verifies the typed URL without saving it", () => {
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "  https://new.example.com/client.json  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Verify" }));

    expect(testState.verifyMutate).toHaveBeenCalledWith({
      request: {
        verifyURLRequestBody: {
          clientIdMetadataUri: "https://new.example.com/client.json",
        },
      },
    });
    expect(testState.createMutate).not.toHaveBeenCalled();
  });

  it("names the client on a successful verify", () => {
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    act(() => {
      testState.verifyOptions?.onSuccess?.({
        verified: true,
        outcome: "valid",
        detail: "The client ID metadata document is reachable and valid.",
        clientName: "Claude Code",
      });
    });

    expect(testState.toastSuccess).toHaveBeenCalledWith(
      "Verified: Claude Code",
    );
    expect(testState.toastError).not.toHaveBeenCalled();
  });

  it("reports a failed verify with the server's detail", () => {
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    act(() => {
      testState.verifyOptions?.onSuccess?.({
        verified: false,
        outcome: "unreachable",
        detail: "The document endpoint returned HTTP 404.",
      });
    });

    // A failed probe is a successful call, so it must surface as an error
    // toast rather than being mistaken for a passing check.
    expect(testState.toastError).toHaveBeenCalledWith(
      "The document endpoint returned HTTP 404.",
    );
    expect(testState.toastSuccess).not.toHaveBeenCalled();
  });

  it("disables Verify until a URL is typed", () => {
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    expect(screen.getByRole("button", { name: "Verify" })).toHaveProperty(
      "disabled",
      true,
    );

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "https://new.example.com/client.json" },
    });

    expect(screen.getByRole("button", { name: "Verify" })).toHaveProperty(
      "disabled",
      false,
    );
  });

  it("disables Verify without the project:write scope even with a URL", () => {
    testState.hasScope = false;
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "https://new.example.com/client.json" },
    });

    expect(screen.getByRole("button", { name: "Verify" })).toHaveProperty(
      "disabled",
      true,
    );
  });

  it("locks the whole add row while a verify is in flight", () => {
    testState.verifyPending = true;
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "https://new.example.com/client.json" },
    });

    // Add must not be reachable mid-verify, or the two mutations race on the
    // same URL and the operator can save before the answer arrives.
    expect(screen.getByRole("button", { name: "Add" })).toHaveProperty(
      "disabled",
      true,
    );
    expect(screen.getByRole("textbox")).toHaveProperty("disabled", true);
    expect(
      screen.getByText(/Checking that the document is reachable and valid/),
    ).toBeDefined();
  });

  it("does not claim to check the document while only Add is in flight", () => {
    testState.createPending = true;
    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    // Create performs no fetch, so promising a reachability check here would
    // be false assurance.
    expect(
      screen.queryByText(/Checking that the document is reachable and valid/),
    ).toBeNull();
  });

  it("removes a URL", () => {
    testState.items = [
      { id: "row-1", clientIdMetadataUri: "https://a.example.com/client.json" },
    ];

    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Remove https://a.example.com/client.json",
      }),
    );

    expect(testState.deleteMutate).toHaveBeenCalledWith({
      request: { id: "row-1" },
    });
  });

  it("disables add and remove without the project:write scope", () => {
    testState.hasScope = false;
    testState.items = [
      { id: "row-1", clientIdMetadataUri: "https://a.example.com/client.json" },
    ];

    render(<CimdCustomClientsField userSessionIssuer={issuer} />);

    expect(screen.getByRole("textbox")).toHaveProperty("disabled", true);
    expect(screen.getByRole("button", { name: "Add" })).toHaveProperty(
      "disabled",
      true,
    );
    expect(
      screen.getByRole("button", {
        name: "Remove https://a.example.com/client.json",
      }),
    ).toHaveProperty("disabled", true);
  });
});
