import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { ClientIdStep, CreateConnectionForm } from "./OktaConnectionForms";
import { makeConnection } from "./testFixtures";

const mutation = vi.hoisted(() => ({
  mutate: vi.fn(),
  isPending: false,
  error: undefined,
}));
vi.mock("@gram/client/react-query/createIdentityProviderConnection.js", () => ({
  useCreateIdentityProviderConnectionMutation: () => mutation,
}));
vi.mock(
  "@gram/client/react-query/submitIdentityProviderConnectionClientId.js",
  () => ({
    useSubmitIdentityProviderConnectionClientIdMutation: () => mutation,
  }),
);
afterEach(cleanup);
beforeEach(() => {
  mutation.mutate.mockClear();
  mutation.isPending = false;
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

describe("ClientIdStep", () => {
  it("guards empty and pending client ID submissions", () => {
    const connection = makeConnection({ clientIdSubmitted: false });
    const { rerender } = render(<ClientIdStep connection={connection} />, {
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
    rerender(<ClientIdStep connection={connection} />);
    expect(input.hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledTimes(1);
  });
});
