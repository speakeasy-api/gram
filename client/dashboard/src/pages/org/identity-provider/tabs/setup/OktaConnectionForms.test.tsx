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
  ReplaceClientSecretForm,
} from "./OktaConnectionForms";
import { makeConnection } from "./testFixtures";

const mutation = vi.hoisted(() => ({
  mutate: vi.fn(),
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
vi.mock(
  "@gram/client/react-query/replaceIdentityProviderConnectionClientSecret.js",
  () => ({
    useReplaceIdentityProviderConnectionClientSecretMutation: (options: {
      onSuccess: typeof mutation.onSuccess;
    }) => {
      mutation.onSuccess = options.onSuccess;
      return mutation;
    },
  }),
);
afterEach(cleanup);
beforeEach(() => {
  mutation.mutate.mockClear();
  mutation.isPending = false;
  mutation.error = undefined;
});

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>{children}</TooltipProvider>
    </QueryClientProvider>
  );
}

describe("CreateConnectionForm", () => {
  it("validates URL in text and guards Enter submissions", () => {
    const { rerender } = render(<CreateConnectionForm />, {
      wrapper: Wrapper,
    });
    const input = screen.getByLabelText("Okta organization URL");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "http://example.okta.com" } });
    expect(screen.getByRole("alert").textContent).toContain(
      "Enter an HTTPS Okta organization URL",
    );
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).not.toHaveBeenCalled();
    fireEvent.change(input, {
      target: { value: " https://example.okta.com/ " },
    });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          createIdentityProviderConnectionRequestBody: {
            orgUrl: "https://example.okta.com",
            listingMode: "custom_app",
          },
        },
      }),
    );
    mutation.isPending = true;
    rerender(<CreateConnectionForm />);
    expect(input.hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledTimes(1);
  });

  it("creates a custom-app connection on click", () => {
    render(<CreateConnectionForm />, { wrapper: Wrapper });
    fireEvent.change(screen.getByLabelText("Okta organization URL"), {
      target: { value: "https://example.okta.com/" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create connection" }));
    expect(mutation.mutate).toHaveBeenCalledExactlyOnceWith({
      security: expect.anything(),
      request: {
        createIdentityProviderConnectionRequestBody: {
          orgUrl: "https://example.okta.com",
          listingMode: "custom_app",
        },
      },
    });
  });

  it("disables the button for a non-Okta URL and while pending", () => {
    const { rerender } = render(<CreateConnectionForm />, {
      wrapper: Wrapper,
    });
    fireEvent.change(screen.getByLabelText("Okta organization URL"), {
      target: { value: "https://unrelated.example.com" },
    });
    const create = screen.getByRole("button", { name: "Create connection" });
    expect(create.hasAttribute("disabled")).toBe(true);
    fireEvent.click(create);
    expect(mutation.mutate).not.toHaveBeenCalled();
    mutation.isPending = true;
    rerender(<CreateConnectionForm />);
    expect(
      screen
        .getByRole("button", { name: "Creating..." })
        .hasAttribute("disabled"),
    ).toBe(true);
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

  it("creates an OIN connection when selected", () => {
    render(<CreateConnectionForm />, { wrapper: Wrapper });
    fireEvent.click(
      screen.getByRole("radio", {
        name: "Okta Integration Network (client secret)",
      }),
    );
    const input = screen.getByLabelText("Okta organization URL");
    fireEvent.change(input, { target: { value: "https://example.okta.com" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          createIdentityProviderConnectionRequestBody: {
            orgUrl: "https://example.okta.com",
            listingMode: "oin",
          },
        },
      }),
    );
  });

  it("requires a masked secret, submits it, and clears it after success", () => {
    const { rerender } = render(<ClientIdForm connection={connection} />, {
      wrapper: Wrapper,
    });
    const id = screen.getByLabelText("Client ID");
    const secret = screen.getByLabelText("Client secret") as HTMLInputElement;
    expect(secret.type).toBe("password");
    fireEvent.change(id, { target: { value: "0oa00000000000000000" } });
    fireEvent.keyDown(id, { key: "Enter" });
    expect(mutation.mutate).not.toHaveBeenCalled();
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

  it("replaces a saved secret without displaying backend error details", () => {
    const saved = { ...connection, clientIdSubmitted: true };
    const { rerender } = render(
      <ReplaceClientSecretForm connection={saved} />,
      { wrapper: Wrapper },
    );
    const secret = screen.getByLabelText(
      "New client secret",
    ) as HTMLInputElement;
    expect(secret.type).toBe("password");
    fireEvent.keyDown(secret, { key: "Enter" });
    expect(mutation.mutate).not.toHaveBeenCalled();
    fireEvent.change(secret, { target: { value: "DEMO_REPLACEMENT_SECRET" } });
    fireEvent.click(
      screen.getByRole("button", { name: "Replace client secret and verify" }),
    );
    expect(mutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          replaceIdentityProviderConnectionClientSecretRequestBody: {
            id: saved.id,
            clientSecret: "DEMO_REPLACEMENT_SECRET",
          },
        },
      }),
    );
    mutation.isPending = true;
    mutation.error = new Error("DEMO_REPLACEMENT_SECRET");
    rerender(<ReplaceClientSecretForm connection={saved} />);
    expect(secret.disabled).toBe(true);
    expect(screen.getByRole("alert").textContent).not.toContain(
      "DEMO_REPLACEMENT_SECRET",
    );
    fireEvent.keyDown(secret, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledTimes(1);
    act(() => mutation.onSuccess?.(makeConnection({ status: "verified" })));
    expect(secret.value).toBe("");
  });

  it.each([
    makeConnection({ listingMode: "oin", clientIdSubmitted: true }),
    makeConnection({ clientIdSubmitted: true }),
    { ...connection, clientIdSubmitted: false },
    { ...connection, clientIdSubmitted: true, status: "revoked" as const },
  ])("does not offer replacement for ineligible connections (%#)", (value) => {
    render(<ReplaceClientSecretForm connection={value} />, {
      wrapper: Wrapper,
    });
    expect(screen.queryByLabelText("New client secret")).toBeNull();
  });
});
