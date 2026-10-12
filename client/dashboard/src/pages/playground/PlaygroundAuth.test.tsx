import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { EnvironmentVariable } from "../mcp/environmentVariableUtils";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Toolset } from "@/lib/toolTypes";
import type { useToolsetMcpTarget } from "@/hooks/useToolsetUrl";

type Target = ReturnType<typeof useToolsetMcpTarget>;
const mocks = vi.hoisted(() => ({
  target: {
    status: "ready" as Target["status"],
    userSessionIssuerId: undefined as string | undefined,
    serverId: "server-s",
    refetch: vi.fn(),
  },
  connection: {
    connected: false,
    needsAuth: true,
    isLoading: false,
    isError: false,
    errorMessage: undefined as string | undefined,
    refetch: vi.fn(),
    connect: vi.fn(),
    canConnect: true,
  },
  envVars: [] as EnvironmentVariable[],
  save: vi.fn(),
  isSaving: false,
  issuerConnection: vi.fn(),
  sessions: vi.fn(),
}));

vi.mock("@/hooks/useToolsetUrl", () => ({
  useToolsetMcpTarget: () => mocks.target,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      details: {
        Link: ({
          children,
          params,
          hash,
        }: {
          children: React.ReactNode;
          params: [string];
          hash?: string;
        }) => (
          <a href={`/mcp/${params[0]}${hash ? `#${hash}` : ""}`}>{children}</a>
        ),
      },
    },
  }),
}));
vi.mock("@gram/client/react-query/getMcpMetadata.js", () => ({
  useGetMcpMetadata: () => ({}),
}));
vi.mock("@gram/client/react-query/listEnvironments.js", () => ({
  useListEnvironments: () => ({}),
}));
vi.mock("@/hooks/useMissingEnvironmentVariables", () => ({
  useMissingRequiredEnvVars: () => 0,
}));
vi.mock("../mcp/useEnvironmentVariables", () => ({
  useEnvironmentVariables: () => mocks.envVars,
}));
vi.mock("./usePlaygroundEnvironment", () => ({
  usePlaygroundEnvironment: () => ({
    exists: false,
    storedEntries: [],
    save: mocks.save,
    isSaving: mocks.isSaving,
  }),
}));
vi.mock("./usePlaygroundIssuerConnection", () => ({
  usePlaygroundIssuerConnection: () => {
    mocks.issuerConnection();
    return mocks.connection;
  },
}));
vi.mock("@/components/sessions/AttachedUserSessions", () => ({
  ToolsetAttachedUserSessions: () => null,
}));
vi.mock("@/components/sessions/ClientsAndSessionsTab", () => ({
  ClientsAndSessionsTab: (props: {
    issuerId?: string;
    originatingMcpServerId?: string;
  }) => {
    mocks.sessions(props);
    return (
      <div>
        {props.issuerId
          ? `Sessions for ${props.issuerId}`
          : "No connections. Enable authentication"}
      </div>
    );
  },
}));

import { PlaygroundAuth } from "./PlaygroundAuth";
import { ToolsetSessionsTab } from "../mcp/ToolsetSessionsTab";

const toolset = {
  id: "toolset-t",
  slug: "legacy-t",
  name: "Test server",
  userSessionIssuerId: "issuer-t",
} as Toolset;

beforeEach(() => {
  vi.clearAllMocks();
  mocks.envVars = [];
  mocks.isSaving = false;
  mocks.save.mockResolvedValue({ created: false });
  mocks.target.status = "ready";
  mocks.target.userSessionIssuerId = undefined;
  mocks.connection.isLoading = false;
  mocks.connection.isError = false;
  mocks.connection.errorMessage = undefined;
});

afterEach(cleanup);

describe("authoritative MCP auth and session presentation", () => {
  it.each([
    ["idle", "Select an MCP server"],
    ["loading", "Loading MCP server authentication"],
    ["error", "Unable to load MCP server authentication"],
    ["unavailable", "This MCP server is disabled"],
  ] as const)(
    "does not make auth decisions while target is %s",
    (status, message) => {
      mocks.target.status = status;
      const auth = renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />);
      const sessions = renderToStaticMarkup(
        <ToolsetSessionsTab toolset={toolset} />,
      );
      for (const html of [auth, sessions]) {
        expect(html).toContain(message);
        expect(html).not.toContain("No authentication required");
        expect(html).not.toContain("No connections");
        expect(html).not.toContain("Enable authentication");
        expect(html).not.toContain("issuer-t");
      }
      expect(mocks.issuerConnection).not.toHaveBeenCalled();
      expect(mocks.sessions).not.toHaveBeenCalled();
    },
  );

  it("renders selected server auth and sessions, not the toolset issuer", () => {
    mocks.target.userSessionIssuerId = "issuer-s";
    const auth = renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />);
    const sessions = renderToStaticMarkup(
      <ToolsetSessionsTab toolset={toolset} />,
    );
    expect(auth).toContain("Not Connected");
    expect(auth).not.toContain("No authentication required");
    expect(mocks.issuerConnection).toHaveBeenCalledOnce();
    expect(sessions).toContain("Sessions for issuer-s");
    expect(sessions).not.toContain("issuer-t");
    expect(mocks.sessions).toHaveBeenCalledWith(
      expect.objectContaining({
        issuerId: "issuer-s",
        originatingMcpServerId: "server-s",
      }),
    );
  });

  it("shows ungated state only after authoritative resolution", () => {
    expect(
      renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />),
    ).toContain("No authentication required");
    expect(
      renderToStaticMarkup(<ToolsetSessionsTab toolset={toolset} />),
    ).toContain("Enable authentication");
    expect(mocks.issuerConnection).not.toHaveBeenCalled();
    expect(mocks.sessions).toHaveBeenCalledWith(
      expect.objectContaining({ issuerId: undefined }),
    );
  });

  it("keeps a pending connection distinct from not connected", () => {
    mocks.target.userSessionIssuerId = "issuer-s";
    mocks.connection.isLoading = true;
    const html = renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />);
    expect(html).not.toContain("Not Connected");
    expect(html).not.toContain("Connection unavailable");
    render(<PlaygroundAuth toolset={toolset} />);
    expect(screen.getByRole("status").textContent).toContain(
      "Checking connection…",
    );
  });

  it("shows probe failure rather than not connected", () => {
    mocks.target.userSessionIssuerId = "issuer-s";
    mocks.connection.isError = true;
    mocks.connection.errorMessage =
      "Unable to check the selected server connection.";
    const html = renderToStaticMarkup(<PlaygroundAuth toolset={toolset} />);
    expect(html).toContain("Connection unavailable");
    expect(html).toContain(mocks.connection.errorMessage);
    expect(html).not.toContain("Not Connected");
    expect(html).toContain("Retry");
  });
});

const editableVariable: EnvironmentVariable = {
  id: "api-key",
  key: "API_KEY",
  state: "user-provided",
  isRequired: true,
  environmentValues: [],
};

describe("independent environment configuration", () => {
  it.each(["idle", "loading", "error", "unavailable", "ready"] as const)(
    "retains editing, saving and navigation when target is %s",
    async (status) => {
      mocks.target.status = status;
      mocks.envVars = [editableVariable];
      render(<PlaygroundAuth toolset={toolset} />);
      const input = screen.getByLabelText("API_KEY") as HTMLInputElement;
      expect(input.disabled).toBe(false);
      expect(
        screen
          .getByRole("link", { name: "Configure auth" })
          .getAttribute("href"),
      ).toBe("/mcp/legacy-t#authentication");
      const save = screen.getByRole("button", {
        name: "Save",
      }) as HTMLButtonElement;
      expect(save.disabled).toBe(true);
      fireEvent.change(input, { target: { value: "test-value" } });
      expect(save.disabled).toBe(false);
      fireEvent.click(save);
      await waitFor(() =>
        expect(mocks.save).toHaveBeenCalledWith(
          [{ name: "API_KEY", value: "test-value" }],
          [],
        ),
      );
    },
  );

  it.each(["idle", "loading", "error", "unavailable", "ready"] as const)(
    "retains navigation without environment variables when target is %s",
    (status) => {
      mocks.target.status = status;
      render(<PlaygroundAuth toolset={toolset} />);
      expect(
        screen.getByRole("link", { name: "Configure auth" }),
      ).toBeDefined();
      if (status !== "ready")
        expect(screen.queryByText("No authentication required")).toBeNull();
      if (status === "loading")
        expect(screen.getByRole("status").textContent).toContain(
          "Loading MCP server authentication",
        );
    },
  );

  it("updates Configure auth navigation when the toolset changes", () => {
    const { rerender } = render(<PlaygroundAuth toolset={toolset} />);
    expect(
      screen.getByRole("link", { name: "Configure auth" }).getAttribute("href"),
    ).toBe("/mcp/legacy-t#authentication");
    rerender(
      <PlaygroundAuth toolset={{ ...toolset, slug: "another-toolset" }} />,
    );
    expect(
      screen.getByRole("link", { name: "Configure auth" }).getAttribute("href"),
    ).toBe("/mcp/another-toolset#authentication");
  });

  it("keeps Save disabled while saving", () => {
    mocks.target.status = "error";
    mocks.envVars = [editableVariable];
    mocks.isSaving = true;
    render(<PlaygroundAuth toolset={toolset} />);
    fireEvent.change(screen.getByLabelText("API_KEY"), {
      target: { value: "test-value" },
    });
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it.each([PlaygroundAuth, ToolsetSessionsTab])(
    "retries the target lookup",
    (Component) => {
      mocks.target.status = "error";
      render(<Component toolset={toolset} />);
      fireEvent.click(screen.getByRole("button", { name: "Retry" }));
      expect(mocks.target.refetch).toHaveBeenCalledOnce();
    },
  );

  it("retries connection failures", () => {
    mocks.target.userSessionIssuerId = "issuer-s";
    mocks.connection.isError = true;
    render(<PlaygroundAuth toolset={toolset} />);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.connection.refetch).toHaveBeenCalledOnce();
    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
  });
});

describe("session target status recovery", () => {
  it.each(["loading", "error", "unavailable"] as const)(
    "suppresses stale issuer and no-issuer session panels while %s",
    (status) => {
      mocks.target.status = status;
      for (const issuer of [undefined, "stale-issuer"]) {
        mocks.target.userSessionIssuerId = issuer;
        const { unmount } = render(<ToolsetSessionsTab toolset={toolset} />);
        expect(
          screen.getByRole(status === "error" ? "alert" : "status"),
        ).toBeDefined();
        expect(
          screen.queryByText(
            /Sessions for|No connections|Enable authentication/,
          ),
        ).toBeNull();
        expect(mocks.sessions).not.toHaveBeenCalled();
        if (status === "unavailable") {
          expect(screen.getByRole("status").textContent).toContain(
            "This MCP server is disabled. Enable it",
          );
          expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
        }
        unmount();
      }
    },
  );
});
