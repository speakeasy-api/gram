import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthenticationSectionBody } from "./AuthenticationSection";
import type { AuthTarget } from "./authTarget";

const {
  attachSheet,
  remoteIdentitySection,
  useAllRemoteSessionClients,
  useUserSessionIssuer,
} = vi.hoisted(() => ({
  attachSheet: vi.fn(),
  remoteIdentitySection: vi.fn(),
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

vi.mock("./AttachRemoteIdentityProviderSheet", () => ({
  AttachRemoteIdentityProviderSheet: (props: { open: boolean }) => {
    attachSheet(props);
    return props.open ? <output>Attach identity provider</output> : null;
  },
}));

vi.mock("./RemoteMcpIdentitySection", () => ({
  RemoteMcpIdentitySectionBody: ({ target }: { target: AuthTarget }) => {
    remoteIdentitySection(target);
    return <output>Remote MCP identity</output>;
  },
}));

vi.mock("./RemoteIdentityProvidersField", () => ({
  RemoteIdentityProvidersField: ({ onAdd }: { onAdd: () => void }) => (
    <button onClick={onAdd}>Add provider</button>
  ),
}));

vi.mock("./AuthenticationSetupActions", () => ({
  AuthenticationSetupActions: ({
    additionalAction,
  }: {
    additionalAction?: ReactNode;
  }) => <>{additionalAction}</>,
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
    children,
  }: {
    onDraftModeChange?: (mode: string) => void;
    children?: ReactNode;
  }) => (
    <div>
      cimd-admission-mode
      <button type="button" onClick={() => onDraftModeChange?.("presets")}>
        draft-presets
      </button>
      <button type="button" onClick={() => onDraftModeChange?.("open")}>
        draft-open
      </button>
      {children}
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
  it("isolates Remote MCP from the standard attach orchestration", () => {
    render(<AuthenticationSectionBody target={remoteMcpTarget} />);

    expect(screen.getByText("Remote MCP identity")).toBeDefined();
    expect(remoteIdentitySection).toHaveBeenCalledWith(remoteMcpTarget);
    expect(attachSheet).not.toHaveBeenCalled();
    expect(useUserSessionIssuer).not.toHaveBeenCalled();
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
    render(
      <AuthenticationSectionBody
        target={standardTargetWithoutSessionIssuer}
        additionalSetupAction={<button>Configure External OAuth</button>}
      />,
    );

    expect(screen.getByText("Configure External OAuth")).toBeDefined();
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
      render(
        <AuthenticationSectionBody target={standardTargetWithSessionIssuer} />,
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
    render(
      <AuthenticationSectionBody target={standardTargetWithSessionIssuer} />,
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
    render(
      <AuthenticationSectionBody target={standardTargetWithSessionIssuer} />,
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
      render(
        <AuthenticationSectionBody target={standardTargetWithSessionIssuer} />,
      );

      expect(screen.getByText("cimd-admission-mode")).toBeDefined();
      expect(screen.queryByText("cimd-custom-clients")).toBeNull();
    },
  );
});

const standardTargetWithSessionIssuer: AuthTarget = {
  kind: "standard",
  slug: "standard-server",
  projectId: "project-1",
  resourceId: "mcp-server-1",
  userSessionIssuerId: "user-session-issuer",
  invalidate: vi.fn(),
};

const remoteMcpTarget: AuthTarget = {
  ...standardTargetWithSessionIssuer,
  kind: "remote-mcp",
  remoteMcpServerId: "remote-mcp-server",
};

const standardTargetWithoutSessionIssuer: AuthTarget = {
  ...standardTargetWithSessionIssuer,
  userSessionIssuerId: null,
};
