import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthenticationSectionBody } from "./AuthenticationSection";
import type { AuthTarget } from "./authTarget";

const {
  useProtectedResourceMetadata,
  useAllRemoteSessionClients,
  useUserSessionIssuer,
} = vi.hoisted(() => ({
  useProtectedResourceMetadata: vi.fn(),
  useAllRemoteSessionClients: vi.fn(),
  useUserSessionIssuer: vi.fn(),
}));

vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: (...args: unknown[]) => useUserSessionIssuer(...args),
}));

vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  useRemoteSessionIssuers: () => ({
    data: { result: { items: [] } },
    isLoading: false,
  }),
}));

vi.mock("./authTarget", () => ({
  useMcpServerAuthTarget: vi.fn(),
}));

vi.mock("./useAllRemoteSessionClients", () => ({
  useAllRemoteSessionClients: (...args: unknown[]) =>
    useAllRemoteSessionClients(...args),
}));

vi.mock("./useProtectedResourceMetadata", () => ({
  useProtectedResourceMetadata: (...args: unknown[]) =>
    useProtectedResourceMetadata(...args),
}));

vi.mock("./AttachRemoteIdentityProviderSheet", () => ({
  AttachRemoteIdentityProviderSheet: ({
    open,
    initialIssuerUrl,
    initialScopes,
  }: {
    open: boolean;
    initialIssuerUrl?: string;
    initialScopes?: string[];
  }) =>
    open ? (
      <output>
        {initialIssuerUrl}|{initialScopes?.join(" ")}
      </output>
    ) : null,
}));

vi.mock("./RemoteIdentityProvidersField", () => ({
  RemoteIdentityProvidersField: ({ onAdd }: { onAdd: () => void }) => (
    <button onClick={onAdd}>Add provider</button>
  ),
}));

vi.mock("./AuthenticationSetupActions", () => ({
  AuthenticationSetupActions: ({
    onUseDiscovered,
    additionalAction,
  }: {
    onUseDiscovered: () => void;
    additionalAction?: ReactNode;
  }) => (
    <>
      <button onClick={onUseDiscovered}>Use Discovered</button>
      {additionalAction}
    </>
  ),
}));

vi.mock("./DeleteRemoteIdentityProviderDialog", () => ({
  DeleteRemoteIdentityProviderDialog: () => null,
}));

vi.mock("./ModifyRemoteIdentityProviderSheet", () => ({
  ModifyRemoteIdentityProviderSheet: () => null,
}));

vi.mock("./UserSessionDurationField", () => ({
  UserSessionDurationField: () => null,
}));

vi.mock("./CimdAdmissionModeField", () => ({
  CimdAdmissionModeField: ({
    onDraftModeChange,
  }: {
    onDraftModeChange?: (mode: string) => void;
  }) => (
    <div>
      cimd-admission-mode
      <button type="button" onClick={() => onDraftModeChange?.("presets")}>
        draft-presets
      </button>
      <button type="button" onClick={() => onDraftModeChange?.("open")}>
        draft-open
      </button>
    </div>
  ),
}));

vi.mock("./CimdCustomClientsField", () => ({
  CimdCustomClientsField: () => <div>cimd-custom-clients</div>,
}));

beforeEach(() => {
  useUserSessionIssuer.mockReturnValue({
    data: {
      id: "user-session-issuer",
      clientIdMetadataAdmissionMode: "reporting",
    },
    isLoading: false,
    isError: false,
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("AuthenticationSectionBody", () => {
  it("preserves scopes when a remote server has a session issuer but no upstream client", () => {
    useAllRemoteSessionClients.mockReturnValue({
      items: [],
      isLoading: false,
    });
    useProtectedResourceMetadata.mockReturnValue({
      status: "available",
      metadata: {
        authorizationServers: ["https://auth.example.com"],
        scopesSupported: ["resource.read", "resource.write"],
      },
    });

    render(
      <AuthenticationSectionBody target={remoteTargetWithSessionIssuer} />,
    );

    expect(useProtectedResourceMetadata).toHaveBeenCalledWith(
      "remote-mcp-server",
      true,
    );

    fireEvent.click(screen.getByText("Add provider"));

    expect(
      screen.getByText("https://auth.example.com|resource.read resource.write"),
    ).toBeDefined();
  });

  it("preserves scopes during first-time setup without a session issuer", () => {
    useUserSessionIssuer.mockReturnValue({
      data: null,
      isLoading: false,
      isError: false,
    });
    useAllRemoteSessionClients.mockReturnValue({
      items: [],
      isLoading: false,
    });
    useProtectedResourceMetadata.mockReturnValue({
      status: "available",
      metadata: {
        authorizationServers: ["https://auth.example.com"],
        scopesSupported: ["resource.read", "resource.write"],
      },
    });

    render(
      <AuthenticationSectionBody target={remoteTargetWithoutSessionIssuer} />,
    );

    expect(useProtectedResourceMetadata).toHaveBeenCalledWith(
      "remote-mcp-server",
      true,
    );

    fireEvent.click(screen.getByText("Use Discovered"));

    expect(
      screen.getByText("https://auth.example.com|resource.read resource.write"),
    ).toBeDefined();
  });

  it("includes a target-specific setup action before a session issuer is configured", () => {
    useUserSessionIssuer.mockReturnValue({
      data: null,
      isLoading: false,
      isError: false,
    });
    useAllRemoteSessionClients.mockReturnValue({
      items: [],
      isLoading: false,
    });
    useProtectedResourceMetadata.mockReturnValue({
      status: "idle",
      metadata: null,
    });

    render(
      <AuthenticationSectionBody
        target={remoteTargetWithoutSessionIssuer}
        additionalSetupAction={<button>Configure External OAuth</button>}
      />,
    );

    expect(screen.getByText("Configure External OAuth")).toBeDefined();
  });

  it("does not probe configured remote servers that already have a client", () => {
    useAllRemoteSessionClients.mockReturnValue({
      items: [{ id: "remote-session-client" }],
      isLoading: false,
    });
    useProtectedResourceMetadata.mockReturnValue({
      status: "idle",
      metadata: null,
    });

    render(
      <AuthenticationSectionBody target={remoteTargetWithSessionIssuer} />,
    );

    expect(useProtectedResourceMetadata).toHaveBeenCalledWith(
      "remote-mcp-server",
      false,
    );
  });

  it.each(["presets", "reporting"])(
    "shows the custom CIMD client list in %s mode",
    (mode) => {
      useUserSessionIssuer.mockReturnValue({
        data: {
          id: "user-session-issuer",
          clientIdMetadataAdmissionMode: mode,
        },
        isLoading: false,
        isError: false,
      });
      useAllRemoteSessionClients.mockReturnValue({
        items: [],
        isLoading: false,
      });
      useProtectedResourceMetadata.mockReturnValue({
        status: "idle",
        metadata: null,
      });

      render(
        <AuthenticationSectionBody target={remoteTargetWithSessionIssuer} />,
      );

      expect(screen.getByText("cimd-admission-mode")).toBeDefined();
      expect(screen.getByText("cimd-custom-clients")).toBeDefined();
    },
  );

  it("reveals the custom CIMD client list for an unsaved Known clients selection", () => {
    useUserSessionIssuer.mockReturnValue({
      data: {
        id: "user-session-issuer",
        clientIdMetadataAdmissionMode: "open",
      },
      isLoading: false,
      isError: false,
    });
    useAllRemoteSessionClients.mockReturnValue({ items: [], isLoading: false });
    useProtectedResourceMetadata.mockReturnValue({
      status: "idle",
      metadata: null,
    });

    render(
      <AuthenticationSectionBody target={remoteTargetWithSessionIssuer} />,
    );

    // Staging the URLs that "Known clients" enforces has to be possible
    // BEFORE the mode is saved, or an operator switches into enforcement
    // with an empty list and races to fill it while clients are being
    // turned away.
    expect(screen.queryByText("cimd-custom-clients")).toBeNull();
    fireEvent.click(screen.getByText("draft-presets"));
    expect(screen.getByText("cimd-custom-clients")).toBeDefined();
  });

  it("hides the custom CIMD client list again when the selection moves off Known clients", () => {
    useUserSessionIssuer.mockReturnValue({
      data: {
        id: "user-session-issuer",
        clientIdMetadataAdmissionMode: "presets",
      },
      isLoading: false,
      isError: false,
    });
    useAllRemoteSessionClients.mockReturnValue({ items: [], isLoading: false });
    useProtectedResourceMetadata.mockReturnValue({
      status: "idle",
      metadata: null,
    });

    render(
      <AuthenticationSectionBody target={remoteTargetWithSessionIssuer} />,
    );

    expect(screen.getByText("cimd-custom-clients")).toBeDefined();
    fireEvent.click(screen.getByText("draft-open"));
    expect(screen.queryByText("cimd-custom-clients")).toBeNull();
  });

  it.each(["open", "disabled"])(
    "hides the custom CIMD client list in %s mode",
    (mode) => {
      useUserSessionIssuer.mockReturnValue({
        data: {
          id: "user-session-issuer",
          clientIdMetadataAdmissionMode: mode,
        },
        isLoading: false,
        isError: false,
      });
      useAllRemoteSessionClients.mockReturnValue({
        items: [],
        isLoading: false,
      });
      useProtectedResourceMetadata.mockReturnValue({
        status: "idle",
        metadata: null,
      });

      render(
        <AuthenticationSectionBody target={remoteTargetWithSessionIssuer} />,
      );

      expect(screen.getByText("cimd-admission-mode")).toBeDefined();
      expect(screen.queryByText("cimd-custom-clients")).toBeNull();
    },
  );
});

const remoteTargetWithSessionIssuer: AuthTarget = {
  slug: "remote-server",
  userSessionIssuerId: "user-session-issuer",
  remoteMcpServerId: "remote-mcp-server",
  invalidate: vi.fn(),
};

const remoteTargetWithoutSessionIssuer: AuthTarget = {
  ...remoteTargetWithSessionIssuer,
  userSessionIssuerId: null,
};
