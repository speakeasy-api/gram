import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { OktaConnectionTab } from "./OktaConnectionTab";
import { ClientIdStep } from "./OktaConnectionSteps";

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

describe("Okta connection forms", () => {
  it("validates URL in text and guards Enter submissions", () => {
    const { rerender } = render(<OktaConnectionTab connection={undefined} />, {
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
    rerender(<OktaConnectionTab connection={undefined} />);
    expect(input.hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(input, { key: "Enter" });
    expect(mutation.mutate).toHaveBeenCalledTimes(1);
  });
  it("guards empty and pending client ID submissions", () => {
    const connection = {
      id: "test-connection",
    } as OktaIdentityProviderConnection;
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
