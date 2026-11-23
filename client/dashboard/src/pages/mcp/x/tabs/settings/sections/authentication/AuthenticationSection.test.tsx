import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthenticationSectionBody } from "./AuthenticationSection";
import type { AuthTarget } from "./authTarget";

const {
  attachSheet,
  saveNoUpstream,
  deniedScopes,
  useServerClientAssociations,
  useRemoteSessionIssuersByIds,
  useUserSessionIssuer,
  useEffectiveUserSessionIssuers,
} = vi.hoisted(() => ({
  attachSheet: vi.fn(),
  saveNoUpstream: vi.fn(),
  deniedScopes: new Set<string>(),
  useServerClientAssociations: vi.fn(),
  useRemoteSessionIssuersByIds: vi.fn(),
  useUserSessionIssuer: vi.fn(),
  useEffectiveUserSessionIssuers: vi.fn(),
}));

vi.mock("@/pages/mcp/gateway/GatewayMemberSignInField", () => ({
  GatewayAuthorizationServersField: () => null,
}));

vi.mock(
  "@gram/client/react-query/setMcpServerUpstreamAuthRequirement.js",
  () => ({
    useSetMcpServerUpstreamAuthRequirementMutation: () => ({
      mutate: saveNoUpstream,
    }),
  }),
);

vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: (...args: unknown[]) => useUserSessionIssuer(...args),
}));

vi.mock("@gram/client/react-query/remoteSessionIssuers.js", () => ({
  useRemoteSessionIssuers: () => ({
    data: { result: { items: [] } },
    isLoading: false,
  }),
}));

vi.mock("@/hooks/useEffectiveUserSessionIssuers", () => ({
  useEffectiveUserSessionIssuers: (...args: unknown[]) =>
    useEffectiveUserSessionIssuers(...args),
}));

vi.mock("./UserSessionIssuerField", () => ({
  UserSessionIssuerField: ({ issuers }: { issuers: Array<{ id: string }> }) => (
    <output>issuers-{issuers.map(({ id }) => id).join(",")}</output>
  ),
}));

vi.mock("./authTarget", () => ({
  useMcpServerAuthTarget: vi.fn(),
}));

vi.mock("@gram/client/react-query/serverClientAssociations.js", () => ({
  useServerClientAssociations: (...args: unknown[]) =>
    useServerClientAssociations(...args),
}));

vi.mock("@/lib/remote-identity/queries/useRemoteSessionIssuersByIds", () => ({
  useRemoteSessionIssuersByIds: (...args: unknown[]) =>
    useRemoteSessionIssuersByIds(...args),
}));

vi.mock("./AttachRemoteIdentityProviderSheet", () => ({
  AttachRemoteIdentityProviderSheet: (props: { open: boolean }) => {
    attachSheet(props);
    return props.open ? <output>Attach identity provider</output> : null;
  },
}));

vi.mock("./RemoteIdentityProvidersField", () => ({
  RemoteIdentityProvidersField: ({
    associatedIssuers,
    isError,
    onAdd,
    readOnly,
  }: {
    associatedIssuers: Array<{ id: string }>;
    isError?: boolean;
    onAdd: () => void;
    readOnly?: boolean;
  }) => (
    <>
      <button onClick={onAdd}>Add authorization server</button>
      <output>providers-{readOnly ? "read-only" : "editable"}</output>
      <output>
        attached-{associatedIssuers.map(({ id }) => id).join(",")}
      </output>
      {isError && <output>attached-error</output>}
    </>
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
  UserSessionDurationField: ({ readOnly }: { readOnly?: boolean }) => (
    <output>duration-{readOnly ? "read-only" : "editable"}</output>
  ),
}));

// The session controls own the footer Save; these cases cover what renders.
vi.mock("@gram/client/react-query/updateUserSessionIssuer.js", () => ({
  useUpdateUserSessionIssuerMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
  }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
    level,
  }: {
    scope: string;
    level: string;
    children: ReactNode | ((props: { disabled: boolean }) => ReactNode);
  }) =>
    level === "section" && deniedScopes.has(scope) ? null : (
      <>
        {typeof children === "function"
          ? children({ disabled: false })
          : children}
      </>
    ),
}));

vi.mock("./CimdAdmissionModeField", () => ({
  CimdAdmissionModeField: ({
    onDraftModeChange,
    children,
    readOnly,
  }: {
    onDraftModeChange?: (mode: string) => void;
    children?: ReactNode;
    readOnly?: boolean;
  }) => (
    <div>
      cimd-admission-mode
      <output>cimd-{readOnly ? "read-only" : "editable"}</output>
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
  useRemoteSessionIssuersByIds.mockImplementation((ids: string[]) => ({
    items: ids.map((id) => ({ id })),
    isLoading: false,
    isError: false,
  }));
  useEffectiveUserSessionIssuers.mockReturnValue({
    issuers: [],
    organizationIssuers: [],
    isLoading: false,
    isError: false,
  });
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
  deniedScopes.clear();
});

describe("AuthenticationSectionBody", () => {
  it("records no upstream authentication on the hosted server only", () => {
    useServerClientAssociations.mockReturnValue({
      data: { associations: [] },
      isLoading: false,
    });
    render(
      <AuthenticationSectionBody target={standardTargetWithSessionIssuer} />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "No upstream authentication" }),
    );

    expect(saveNoUpstream).toHaveBeenCalledWith({
      request: {
        setMcpServerUpstreamAuthRequirementRequestBody: {
          mcpServerId:
            standardTargetWithSessionIssuer.mcpServerId ??
            standardTargetWithSessionIssuer.attachImpactTarget.toolsetId,
          requiresUpstreamAuth: false,
        },
      },
    });
  });

  it.each(["non-hosted", "assigned", "gateway", "org:admin", "mcp:write"])(
    "does not offer hosted opt-out for %s",
    (scenario) => {
      const target = { ...standardTargetWithSessionIssuer };
      if (scenario === "non-hosted") target.hosted = false;
      if (scenario === "gateway")
        target.attachImpactTarget = { metaMcpServerId: "gateway-1" };
      if (scenario.includes(":")) deniedScopes.add(scenario);
      useServerClientAssociations.mockReturnValue({
        data: {
          associations:
            scenario === "assigned"
              ? [{ remoteSessionIssuerId: "provider-1" }]
              : [],
        },
        isLoading: false,
      });
      render(<AuthenticationSectionBody target={target} />);
      expect(
        screen.queryByRole("button", { name: "No upstream authentication" }),
      ).toBeNull();
    },
  );

  it("shows a saved upstream registration before downstream sign-in is configured", () => {
    useServerClientAssociations.mockReturnValue({
      data: { associations: [{ remoteSessionIssuerId: "saved-provider" }] },
      isLoading: false,
    });
    render(
      <AuthenticationSectionBody target={standardTargetWithoutSessionIssuer} />,
    );
    expect(screen.getByText("attached-saved-provider")).toBeDefined();
    expect(
      screen.queryByRole("button", { name: "No upstream authentication" }),
    ).toBeNull();
  });

  it("includes a target-specific setup action before a session issuer is configured", () => {
    useUserSessionIssuer.mockReturnValue({
      data: null,
      isLoading: false,
      isError: false,
    });
    useServerClientAssociations.mockReturnValue({
      data: { associations: [] },
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

  it("renders an attached provider the issuer listing does not contain", () => {
    // The listing mock above returns no issuers at all, standing in for an
    // attached issuer pushed past the first page by the platform catalog.
    useServerClientAssociations.mockReturnValue({
      data: {
        associations: [
          {
            remoteSessionClientId: "client-1",
            remoteSessionIssuerId: "issuer-far",
          },
        ],
      },
      isLoading: false,
    });
    render(
      <AuthenticationSectionBody target={standardTargetWithSessionIssuer} />,
    );

    expect(useRemoteSessionIssuersByIds).toHaveBeenLastCalledWith(
      ["issuer-far"],
      { enabled: true },
    );
    expect(screen.getByText("attached-issuer-far")).toBeDefined();
  });

  it("reports a failed provider lookup instead of an empty provider list", () => {
    useServerClientAssociations.mockReturnValue({
      data: {
        associations: [
          {
            remoteSessionClientId: "client-1",
            remoteSessionIssuerId: "issuer-gone",
          },
        ],
      },
      isLoading: false,
    });
    useRemoteSessionIssuersByIds.mockReturnValue({
      items: [],
      isLoading: false,
      isError: true,
    });
    render(
      <AuthenticationSectionBody target={standardTargetWithSessionIssuer} />,
    );

    expect(screen.getByText("attached-error")).toBeDefined();
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
      useServerClientAssociations.mockReturnValue({
        data: { associations: [] },
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
    useServerClientAssociations.mockReturnValue({
      data: { associations: [] },
      isLoading: false,
    });
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
    useServerClientAssociations.mockReturnValue({
      data: { associations: [] },
      isLoading: false,
    });
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
      useServerClientAssociations.mockReturnValue({
        data: { associations: [] },
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
  hosted: true,
  slug: "standard-server",
  projectId: "project-1",
  permissionResourceId: "mcp-server-1",
  mcpServerId: "mcp-server-1",
  attachImpactTarget: { mcpServerId: "mcp-server-1" },
  supportsOrganizationIssuers: true,
  userSessionIssuerId: "user-session-issuer",
  invalidate: vi.fn(),
};

const standardTargetWithoutSessionIssuer: AuthTarget = {
  ...standardTargetWithSessionIssuer,
  userSessionIssuerId: null,
};
