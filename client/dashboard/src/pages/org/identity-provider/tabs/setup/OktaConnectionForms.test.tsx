import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import {
  ClientIdForm,
  CreateConnectionForm,
  SetupMethodChooser,
} from "./OktaConnectionForms";
import { makeConnection } from "./testFixtures";

const mutation = vi.hoisted(() => ({
  mutate: vi.fn(),
  reset: vi.fn(),
  isPending: false,
  error: undefined as Error | undefined,
  onSuccess: undefined as
    | undefined
    | ((updated: ReturnType<typeof makeConnection>) => void),
}));
vi.mock("@gram/client/react-query/createIdentityProviderConnection.js", () => ({
  useCreateIdentityProviderConnectionMutation: () => mutation,
}));
vi.mock(
  "@gram/client/react-query/setIdentityProviderConnectionSetupMethod.js",
  () => ({
    useSetIdentityProviderConnectionSetupMethodMutation: () => mutation,
  }),
);
vi.mock(
  "@gram/client/react-query/submitIdentityProviderConnectionClientId.js",
  () => ({
    useSubmitIdentityProviderConnectionClientIdMutation: (options: {
      onSuccess: typeof mutation.onSuccess;
    }) => {
      mutation.onSuccess = options.onSuccess;
      return mutation;
    },
  }),
);
afterEach(cleanup);
beforeEach(() => {
  mutation.mutate.mockReset();
  mutation.isPending = false;
  mutation.error = undefined;
  mutation.onSuccess = undefined;
});

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>{children}</TooltipProvider>
    </QueryClientProvider>
  );
}

describe("CreateConnectionForm", () => {
  it("validates the URL and creates an OIN connection", () => {
    const { rerender } = render(<CreateConnectionForm />, {
      wrapper: Wrapper,
    });
    expect(screen.queryByRole("radio")).toBeNull();
    const input = screen.getByLabelText("Okta organization URL");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "http://example.okta.com" } });
    expect(screen.getByRole("alert").textContent).toContain(
      "Enter an HTTPS Okta organization or Admin Console URL",
    );
    expect(
      screen.getByRole("button", { name: "Continue" }).hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.change(input, {
      target: { value: " https://example-admin.okta.com/admin/home " },
    });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledExactlyOnceWith({
      security: expect.anything(),
      request: {
        createIdentityProviderConnectionRequestBody: {
          orgUrl: "https://example.okta.com",
          listingMode: "oin",
        },
      },
    });
    mutation.isPending = true;
    rerender(<CreateConnectionForm />);
    expect(input.hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledTimes(1);
  });

  it("shows the org URL for a pasted admin console URL", () => {
    render(<CreateConnectionForm />, { wrapper: Wrapper });
    const input = screen.getByLabelText<HTMLInputElement>(
      "Okta organization URL",
    );
    fireEvent.change(input, {
      target: { value: "https://example-admin.okta.com/admin/home" },
    });
    fireEvent.blur(input);
    expect(input.value).toBe("https://example.okta.com");
  });
});

describe("SetupMethodChooser", () => {
  const oin = makeConnection({
    status: "pending",
    clientIdSubmitted: false,
    listingMode: "oin",
    jwksUrl: undefined,
  });

  it("marks the connection's method and recommends OIN", () => {
    render(<SetupMethodChooser connection={oin} />, { wrapper: Wrapper });
    expect(
      screen
        .getByRole("radio", { name: /Okta Integration Network/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(screen.getByText("Recommended")).toBeTruthy();
  });

  it("switches the pending connection's method in place", () => {
    render(<SetupMethodChooser connection={oin} />, { wrapper: Wrapper });
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Custom API Services app (private key)",
      }),
    );
    expect(mutation.mutate).toHaveBeenCalledExactlyOnceWith({
      security: expect.anything(),
      request: {
        setIdentityProviderConnectionSetupMethodRequestBody: {
          id: oin.id,
          listingMode: "custom_app",
        },
      },
    });
  });

  it("ignores a click on the current method", () => {
    render(<SetupMethodChooser connection={oin} />, { wrapper: Wrapper });
    fireEvent.click(
      screen.getByRole("radio", { name: /Okta Integration Network/ }),
    );
    expect(mutation.mutate).not.toHaveBeenCalled();
  });

  it("does nothing while a switch is pending", () => {
    mutation.isPending = true;
    render(<SetupMethodChooser connection={oin} />, { wrapper: Wrapper });
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Custom API Services app (private key)",
      }),
    );
    expect(mutation.mutate).not.toHaveBeenCalled();
  });
});

describe("ClientIdForm", () => {
  it("guards empty and pending client ID submissions", () => {
    const connection = makeConnection({ clientIdSubmitted: false });
    const { rerender } = render(<ClientIdForm connection={connection} />, {
      wrapper: Wrapper,
    });
    const input = screen.getByLabelText("Client ID");
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "wlp00000000000000000" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toContain(
      "starting with 0oa",
    );
    fireEvent.change(input, { target: { value: " 0oa00000000000000000 " } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          submitIdentityProviderConnectionClientIDRequestBody: {
            id: connection.id,
            clientId: "0oa00000000000000000",
          },
        },
      }),
    );
    mutation.isPending = true;
    rerender(<ClientIdForm connection={connection} />);
    expect(input.hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledTimes(1);
  });
});

describe("OIN credentials", () => {
  const connection = makeConnection({ listingMode: "oin", jwksUrl: undefined });

  it("requires a masked secret, submits it, and clears it after success", () => {
    const { rerender } = render(<ClientIdForm connection={connection} />, {
      wrapper: Wrapper,
    });
    const id = screen.getByLabelText("Client ID");
    const secret = screen.getByLabelText("Client secret") as HTMLInputElement;
    expect(secret.type).toBe("password");
    expect(secret.autocomplete).toBe("new-password");
    fireEvent.change(id, { target: { value: "0oa00000000000000000" } });
    fireEvent.keyDown(id, { key: "Enter" });
    expect(mutation.mutate).not.toHaveBeenCalled();
    expect(
      screen
        .getByRole("button", { name: "Submit and verify" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.change(secret, { target: { value: "DEMO_CLIENT_SECRET" } });
    fireEvent.keyDown(secret, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          submitIdentityProviderConnectionClientIDRequestBody: {
            id: connection.id,
            clientId: "0oa00000000000000000",
            clientSecret: "DEMO_CLIENT_SECRET",
          },
        },
      }),
    );
    mutation.isPending = true;
    rerender(<ClientIdForm connection={connection} />);
    expect(secret.disabled).toBe(true);
    fireEvent.keyDown(secret, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledTimes(1);
    act(() => mutation.onSuccess?.(makeConnection({ status: "verified" })));
    expect(secret.value).toBe("");
  });

  it("keeps legacy OIN connections on private-key authentication", () => {
    const legacy = makeConnection({ listingMode: "oin" });
    render(<ClientIdForm connection={legacy} />, { wrapper: Wrapper });
    expect(screen.queryByLabelText("Client secret")).toBeNull();
    const id = screen.getByLabelText("Client ID");
    fireEvent.change(id, { target: { value: "0oa00000000000000000" } });
    fireEvent.keyDown(id, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          submitIdentityProviderConnectionClientIDRequestBody: {
            id: legacy.id,
            clientId: "0oa00000000000000000",
          },
        },
      }),
    );
  });
});
