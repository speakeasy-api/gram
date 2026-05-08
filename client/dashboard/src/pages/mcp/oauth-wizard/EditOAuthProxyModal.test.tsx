import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  updateOAuthProxy: vi.fn().mockResolvedValue(undefined),
  capture: vi.fn(),
  invalidateAllToolset: vi.fn(),
}));

vi.mock("@gram/client/react-query", () => ({
  invalidateAllToolset: mocks.invalidateAllToolset,
  useUpdateOAuthProxyServerMutation: (options: {
    onSuccess?: () => void;
    onError?: (e: Error) => void;
  }) => ({
    mutate: (vars: unknown) => {
      mocks.updateOAuthProxy(vars).then(
        () => options.onSuccess?.(),
        (e: Error) => options.onError?.(e),
      );
    },
    isPending: false,
  }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: mocks.capture }),
}));

vi.mock("@speakeasy-api/moonshine", () => ({
  Button: ({
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
  Stack: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import { EditOAuthProxyModal } from "./EditOAuthProxyModal";

const proxyServer = {
  slug: "existing",
  audience: "aud-original",
  oauthProxyProviders: [
    {
      authorizationEndpoint: "https://e.example/auth",
      tokenEndpoint: "https://e.example/token",
      scopesSupported: ["read", "write"],
      tokenEndpointAuthMethodsSupported: ["client_secret_basic"],
      environmentSlug: "env-existing",
    },
  ],
} as unknown as Parameters<typeof EditOAuthProxyModal>[0]["proxyServer"];

function renderModal(
  props: Partial<Parameters<typeof EditOAuthProxyModal>[0]> = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <EditOAuthProxyModal
          isOpen
          onClose={() => {}}
          toolsetSlug="mytoolset"
          proxyServer={proxyServer}
          {...props}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  mocks.updateOAuthProxy.mockClear();
  mocks.capture.mockClear();
  mocks.invalidateAllToolset.mockClear();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("EditOAuthProxyModal", () => {
  it("pre-fills fields from the existing proxy server", () => {
    renderModal();
    expect(
      (
        screen.getByPlaceholderText(
          "https://provider.com/oauth/authorize",
        ) as HTMLInputElement
      ).value,
    ).toBe("https://e.example/auth");
    expect(
      (
        screen.getByPlaceholderText(
          "https://provider.com/oauth/token",
        ) as HTMLInputElement
      ).value,
    ).toBe("https://e.example/token");
    expect(
      (screen.getByPlaceholderText("read, write, openid") as HTMLInputElement)
        .value,
    ).toBe("read, write");
    expect(
      (
        screen.getByPlaceholderText(
          "https://api.example.com",
        ) as HTMLInputElement
      ).value,
    ).toBe("aud-original");
    // slug is read-only
    const slugInput = screen
      .getAllByRole("textbox")
      .find((el) => (el as HTMLInputElement).value === "existing");
    expect(slugInput).toBeTruthy();
    expect((slugInput as HTMLInputElement).disabled).toBe(true);
  });

  it("submits the mutation with audience set when changed", async () => {
    const onClose = vi.fn();
    renderModal({ onClose });

    const audienceInput = screen.getByPlaceholderText(
      "https://api.example.com",
    );
    fireEvent.change(audienceInput, { target: { value: "aud-changed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => {
      expect(mocks.updateOAuthProxy).toHaveBeenCalledTimes(1);
    });

    const call = mocks.updateOAuthProxy.mock.calls[0][0];
    expect(
      call.request.updateOAuthProxyServerRequestBody.oauthProxyServer.audience,
    ).toBe("aud-changed");

    await waitFor(() => {
      expect(mocks.invalidateAllToolset).toHaveBeenCalledTimes(1);
      expect(mocks.capture).toHaveBeenCalledWith(
        "mcp_event",
        expect.objectContaining({ action: "oauth_proxy_updated" }),
      );
      expect(onClose).toHaveBeenCalledTimes(1);
    });
  });

  it("omits audience from the mutation when unchanged", async () => {
    renderModal();

    fireEvent.change(
      screen.getByPlaceholderText("https://provider.com/oauth/authorize"),
      { target: { value: "https://e.example/new-auth" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => {
      expect(mocks.updateOAuthProxy).toHaveBeenCalledTimes(1);
    });

    const body =
      mocks.updateOAuthProxy.mock.calls[0][0].request
        .updateOAuthProxyServerRequestBody.oauthProxyServer;
    expect(body.audience).toBeUndefined();
    expect(body.authorizationEndpoint).toBe("https://e.example/new-auth");
  });

  it("submits with empty scopes when the field is cleared (scopes are optional)", async () => {
    renderModal();

    fireEvent.change(screen.getByPlaceholderText("read, write, openid"), {
      target: { value: " , " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => {
      expect(mocks.updateOAuthProxy).toHaveBeenCalledTimes(1);
    });
    const body =
      mocks.updateOAuthProxy.mock.calls[0][0].request
        .updateOAuthProxyServerRequestBody.oauthProxyServer;
    expect(body.scopesSupported).toEqual([]);
  });
});
