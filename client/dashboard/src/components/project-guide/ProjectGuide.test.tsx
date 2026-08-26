import {
  act,
  cleanup,
  fireEvent,
  render as baseRender,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import { ProjectGuideRun } from "./ProjectGuideRun";
import {
  PROJECT_GUIDE_JOURNEYS,
  type JourneyId,
  type JourneyStatus,
} from "./journeys";
import {
  LISTEN_TIMEOUT_SECONDS,
  PROJECT_GUIDE_MICRO_STEP_DELAY_MS,
} from "./projectGuideMachine";
import type {
  ProjectGuideOperationReport,
  ProjectGuideOperationScope,
  ProjectGuideOperationSignal,
} from "./projectGuideMachine";

const statusByJourney = vi.hoisted(
  () =>
    ({
      current: {
        "third-party-mcp": "not-started",
        "secret-block": "not-started",
      },
    }) as { current: Record<JourneyId, JourneyStatus> },
);
const progressPending = vi.hoisted(() => ({ current: false }));
const navigate = vi.hoisted(() => vi.fn());
const slugs = vi.hoisted(() => ({
  current: { orgSlug: "org", projectSlug: "project-guide-test" },
}));
const mcpOperations = vi.hoisted(() => ({
  current: {} as Record<string, unknown>,
}));
const secretOperations = vi.hoisted(() => ({
  current: {} as Record<string, unknown>,
}));

vi.mock("./useProjectGuideProgress", () => ({
  useProjectGuideProgress: () => ({
    statusByJourney: statusByJourney.current,
    isPending: progressPending.current,
  }),
}));
vi.mock("./projectGuideStores", () => ({
  markProjectGuideStarted: vi.fn(),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => slugs.current,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    home: { href: () => "/org/projects/project-guide-test" },
  }),
}));

vi.mock("./useMcpGuideOperations", () => ({
  MCP_GUIDE_CLIENTS: ["claude", "codex", "cursor"],
  useMcpGuideOperations: () => mcpOperations.current,
}));

vi.mock("./useSecretGuideOperations", () => ({
  SECRET_GUIDE_CLIENTS: {
    claude: { label: "Claude Code", directory: "~/.claude/plugins/" },
    codex: { label: "Codex", directory: "~/.codex/plugins/" },
    cursor: { label: "Cursor", directory: "~/.cursor/extensions/" },
  },
  useSecretGuideOperations: () => secretOperations.current,
}));

vi.mock("react-router", () => ({
  Link: ({
    children,
    to,
    ...props
  }: {
    children: React.ReactNode;
    to: string;
  }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
  useNavigate: () => navigate,
}));

import { ProjectGuide } from "./ProjectGuide";

function render(ui: React.ReactNode) {
  return baseRender(ui, {
    wrapper: ({ children }) => (
      <ConfigProvider theme="light" setTheme={() => undefined}>
        {children}
      </ConfigProvider>
    ),
  });
}

async function advanceGuideDelay(count = 1): Promise<void> {
  await act(() =>
    vi.advanceTimersByTime(count * PROJECT_GUIDE_MICRO_STEP_DELAY_MS),
  );
}

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (value: T) => void;
} {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

const catalogServer = {
  description: "Read-only test server",
  registryId: "registry",
  registrySpecifier: "example/read-only",
  version: "1.0.0",
  title: "Linear",
  meta: {},
  toolCount: 2,
  isReadOnly: true,
  supportsDcr: true,
  remotes: [
    {
      transportType: "streamable-http",
      url: "https://upstream.example/mcp",
    },
  ],
};

function resetMcpOperations(): void {
  mcpOperations.current = {
    activityBaselineError: false,
    activityBaselinePending: false,
    catalogError: false,
    catalogPending: false,
    catalogServers: [catalogServer],
    client: "claude",
    connectionPromptCopied: true,
    promptCopied: false,
    endpointUrl: "https://api.example/mcp/linear-endpoint",
    handleSignal: vi.fn(),
    markConnectionPromptCopied: vi.fn(),
    markPromptCopied: vi.fn(),
    mcpServer: {
      id: "mcp-server-id",
      slug: "linear-governed",
      name: "Linear",
      remoteMcpServerId: "remote-id",
    },
    serverName: "Linear",
    projectStateError: false,
    projectStatePending: false,
    prompt:
      "Using the Linear_Governed MCP server at this exact URL, https://api.example/mcp/linear-endpoint, first list the available tools. If multiple servers have the same name, use only the one at this URL. Then choose one tool marked read-only and call it with a harmless request. Do not create, update, or delete anything.",
    retryCatalog: vi.fn(),
    prepareActivityBaseline: vi.fn(() => Promise.resolve(true)),
    selectServer: vi.fn(),
    selectedServer: catalogServer,
    setClient: vi.fn(),
    connectionPrompts: {
      claude:
        "claude mcp add --transport http --scope user 'Linear_Governed' 'https://api.example/mcp/linear-endpoint'",
      cursor:
        '{\n  "mcpServers": {\n    "Linear_Governed": {\n      "type": "http",\n      "url": "https://api.example/mcp/linear-endpoint"\n    }\n  }\n}',
      codex:
        "codex mcp add 'Linear_Governed' --url 'https://api.example/mcp/linear-endpoint'",
    },
    toolLogsHref: "/projects/request-project/logs",
  };
}

function resetSecretOperations(): void {
  secretOperations.current = {
    baselineError: false,
    baselinePending: false,
    client: "claude",
    clientSelected: true,
    downloadedFilename: "gram-observability.zip",
    handleSignal: vi.fn(),
    installCommand: "unzip -oq gram-observability.zip -d ~/.claude/plugins/",
    policyError: false,
    policyPending: false,
    promptCopied: false,
    prompt:
      'Run this exact command in your shell:\n\necho "GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab"',
    retryPolicy: vi.fn(),
    prepareTelemetryBaseline: vi.fn(() => Promise.resolve(true)),
    riskEventsHref: "/projects/request-project/security/events",
    setClient: vi.fn((client: "claude" | "cursor" | "codex") => {
      secretOperations.current.client = client;
    }),
    markPromptCopied: vi.fn(),
  };
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.clearAllMocks();
  progressPending.current = false;
  statusByJourney.current = {
    "third-party-mcp": "not-started",
    "secret-block": "not-started",
  };
  slugs.current = { orgSlug: "org", projectSlug: "project-guide-test" };
  resetMcpOperations();
  resetSecretOperations();
});

resetMcpOperations();
resetSecretOperations();

describe("ProjectGuide", () => {
  it("resets in-memory state when the project changes", () => {
    const { rerender } = render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    expect(
      screen.getByRole("heading", { name: "Govern a third-party MCP" }),
    ).toBeTruthy();

    slugs.current = { orgSlug: "org", projectSlug: "another-project" };
    rerender(<ProjectGuide />);

    expect(
      screen.getByRole("heading", {
        name: "Put your agent traffic under control",
      }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("heading", { name: "Govern a third-party MCP" }),
    ).toBeNull();
  });

  it("renders the approved opening with both selectable journeys", () => {
    render(<ProjectGuide />);

    expect(
      screen.getByRole("heading", {
        name: "Put your agent traffic under control",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    ).toBeTruthy();
    expect(
      screen.getByTestId("project-guide-secret-block-card").textContent,
    ).toContain("Not started");
    expect(
      screen.getByTestId("project-guide-third-party-mcp-card").textContent,
    ).toContain("Not started");
    for (const [journeyId, stepCount] of [
      ["secret-block", 5],
      ["third-party-mcp", 4],
    ] as const) {
      const progressSegments = screen
        .getByTestId(`project-guide-${journeyId}-card`)
        .querySelectorAll(".h-1.w-4");
      expect(progressSegments).toHaveLength(stepCount);
      for (const segment of progressSegments) {
        expect(segment.className).toContain("bg-surface-tertiary-default");
      }
    }
  });

  it("animates only journeys that have not started", () => {
    const { rerender } = render(<ProjectGuide />);

    expect(
      screen
        .getByTestId("project-guide-graphic-third-party-mcp")
        .getAttribute("data-animated"),
    ).toBe("true");

    statusByJourney.current = {
      "third-party-mcp": "in-progress",
      "secret-block": "not-started",
    };
    rerender(<ProjectGuide />);

    expect(
      screen
        .getByTestId("project-guide-graphic-third-party-mcp")
        .getAttribute("data-animated"),
    ).toBe("false");
    expect(
      screen
        .getByTestId("project-guide-graphic-secret-block")
        .getAttribute("data-animated"),
    ).toBe("true");
  });

  it("keeps the other journey switchable when a selected path opens", () => {
    render(<ProjectGuide />);

    const openingControl = screen.getByRole("button", {
      name: /Govern a third-party MCP/,
    });
    const controlledRegionId = openingControl.getAttribute("aria-controls");
    expect(openingControl.getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(openingControl);

    const activeRegion = screen.getByRole("region", {
      name: "Govern a third-party MCP",
    });
    expect(activeRegion.id).toBe(controlledRegionId);
    expect(
      screen
        .getByRole("button", { name: "← Back to start" })
        .getAttribute("aria-expanded"),
    ).toBe("true");
    expect(
      screen
        .getByRole("button", { name: "← Back to start" })
        .getAttribute("aria-controls"),
    ).toBe(controlledRegionId);
    expect(screen.queryByRole("button", { name: "Exit guide" })).toBeNull();
    expect(
      screen.getAllByRole("button", { name: "Start the journey" }),
    ).toHaveLength(1);

    const switchControl = screen.getByRole("button", {
      name: "Switch to Block a leaked credential mid-prompt",
    });
    expect(switchControl.getAttribute("aria-expanded")).toBe("false");
    expect(switchControl.getAttribute("aria-controls")).toBe(
      "project-guide-secret-block-content",
    );

    expect(
      screen.getByRole("heading", { name: "Govern a third-party MCP" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", {
        name: "Switch to Block a leaked credential mid-prompt",
      }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "The catalog lists servers from the official MCP Registry. Installing one creates a governed endpoint in front of the vendor's server — the vendor's URL is already known, and nothing upstream changes.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("What the wizard does")).toBeTruthy();
    expect(screen.getByText("Read the server's tool list")).toBeTruthy();
    expect(screen.getByText("Install it into this project")).toBeTruthy();
    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("Ready to start");

    fireEvent.click(switchControl);

    expect(
      screen.getByRole("heading", {
        name: "Block a leaked credential mid-prompt",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("region", {
        name: "Block a leaked credential mid-prompt",
      }).id,
    ).toBe("project-guide-secret-block-content");
  });

  it("dispatches START and the matching operation signal from the primary button", () => {
    const signals: ProjectGuideOperationSignal[] = [];
    mcpOperations.current.handleSignal = vi.fn(
      (signal: ProjectGuideOperationSignal) => {
        signals.push(signal);
      },
    );
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "running",
    );
    expect(signals).toEqual([
      {
        type: "start",
        scope: { path: "third-party-mcp", step: 0, attempt: 0, runId: 1 },
      },
    ]);
    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("Starting…");
  });

  it("paces automated MCP sub-steps before advancing the step", async () => {
    vi.useFakeTimers();
    mcpOperations.current.handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "prepare") {
          report({
            type: "success",
            scope: signal.scope,
            result: "Linear mcp server is now setup",
          });
          return;
        }
        if (signal.type !== "start" || signal.scope.step !== 0) return;
        report({
          type: "progress",
          scope: signal.scope,
          message: "Read the server's tool list",
          progress: 0.2,
        });
        report({
          type: "progress",
          scope: signal.scope,
          message: "Adding Linear MCP server to this project",
          progress: 0.5,
        });
        report({
          type: "success",
          scope: signal.scope,
          result: "Server installed",
        });
      },
    );
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));

    expect(
      (
        screen.getByRole("button", {
          name: /Linear.*2 tools/,
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);

    const activity = screen.getByRole("log", { name: "Journey A activity" });
    await act(() => vi.advanceTimersByTime(0));
    expect(activity.textContent).toContain("Read the server's tool list");
    expect(activity.textContent).not.toContain(
      "Adding Linear MCP server to this project",
    );
    await act(() =>
      vi.advanceTimersByTime(PROJECT_GUIDE_MICRO_STEP_DELAY_MS - 1),
    );
    expect(activity.textContent).not.toContain(
      "Adding Linear MCP server to this project",
    );
    await act(() => vi.advanceTimersByTime(1));
    expect(activity.textContent).toContain(
      "Adding Linear MCP server to this project",
    );
    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "running",
    );
    await act(() => vi.advanceTimersByTime(PROJECT_GUIDE_MICRO_STEP_DELAY_MS));
    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "checkpoint",
    );
  });

  it("renders checkpoint, waiting, and observed-event completion from adapter reports", async () => {
    vi.useFakeTimers();
    let report: (report: ProjectGuideOperationReport) => void = () => undefined;
    let activeScope: ProjectGuideOperationScope | null = null;
    mcpOperations.current.handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        sendReport: (report: ProjectGuideOperationReport) => void,
      ) => {
        report = sendReport;
        activeScope = signal.scope;
        if (signal.type === "prepare") {
          sendReport({
            type: "success",
            scope: signal.scope,
            result: "Linear mcp server is now setup",
          });
          return;
        }
        if (signal.type !== "start") return;
        if (signal.scope.step === 0) {
          sendReport({
            type: "success",
            scope: signal.scope,
            result: "Server installed",
          });
        }
        if (signal.scope.step === 1) {
          sendReport({
            type: "success",
            scope: signal.scope,
            result: "Endpoint verified",
          });
        }
      },
    );
    const view = render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "checkpoint",
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: "Server is installed",
      }),
    );
    await advanceGuideDelay();
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    mcpOperations.current.promptCopied = true;
    view.rerender(<ProjectGuide />);
    fireEvent.click(screen.getByRole("button", { name: "Prompt run" }));

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "waiting",
    );
    expect(screen.queryByText("Your turn · Connect your client")).toBeNull();
    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("Next · Connect your agent to this server");
    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("Next · Connect your agent to this server");
    if (!activeScope) throw new Error("expected waiting operation scope");
    const waitingScope = activeScope;

    act(() => {
      report({
        type: "event",
        scope: waitingScope,
        event: {
          kind: "Governed call",
          tone: "allow",
          title: "linear.tools/list",
          rows: [{ key: "access", value: "allowed" }],
          note: "Recorded before forwarding.",
        },
      });
    });
    await advanceGuideDelay();

    expect(
      screen.getByRole("heading", { name: "The path is governed." }),
    ).toBeTruthy();
    expect(screen.getByTestId("project-guide-run")).toBeTruthy();
    expect(
      screen.getByRole("heading", { name: "Journey complete" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("linear.tools/list");
    expect(document.querySelectorAll('[aria-current="step"]')).toHaveLength(1);
    expect(screen.getByText("linear.tools/list")).toBeTruthy();
  });

  it("captures the governed-call baseline after MCP setup completes", async () => {
    vi.useFakeTimers();
    const baseline = deferred<boolean>();
    mcpOperations.current.prepareActivityBaseline = vi.fn(
      () => baseline.promise,
    );
    mcpOperations.current.handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "prepare") {
          void (
            mcpOperations.current
              .prepareActivityBaseline as () => Promise<boolean>
          )().then((ready) => {
            if (ready) {
              report({
                type: "success",
                scope: signal.scope,
                result: "Linear mcp server is now setup",
              });
            } else {
              report({
                type: "error",
                scope: signal.scope,
                message: "We couldn't prepare the connection yet. Try again.",
              });
            }
          });
          return;
        }
        if (signal.type === "start" && signal.scope.step === 0) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Linear mcp server is now setup",
          });
        }
      },
    );

    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();

    expect(
      mcpOperations.current.prepareActivityBaseline,
    ).toHaveBeenCalledOnce();
    expect(mcpOperations.current.handleSignal).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "prepare",
        scope: { path: "third-party-mcp", step: 0, attempt: 0, runId: 1 },
      }),
      expect.any(Function),
    );
    expect(screen.queryByText(/first list the available tools/)).toBeNull();
    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "preparing",
    );

    await act(async () => baseline.resolve(true));

    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("Linear mcp server is now setup");

    fireEvent.click(
      screen.getByRole("button", {
        name: "Server is installed",
      }),
    );
    expect(
      screen.getByRole("heading", { name: "Ask the agent to list the tools" }),
    ).toBeTruthy();
    expect(
      mcpOperations.current.prepareActivityBaseline,
    ).toHaveBeenCalledOnce();
  });

  it("does not recapture the MCP baseline from the client checkpoint", () => {
    statusByJourney.current["third-party-mcp"] = "in-progress";
    mcpOperations.current.activityBaselineError = true;
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: "Server is installed",
      }),
    );

    expect(
      mcpOperations.current.prepareActivityBaseline,
    ).not.toHaveBeenCalled();
    expect(
      screen.getByRole("heading", { name: "Ask the agent to list the tools" }),
    ).toBeTruthy();
  });

  it("awaits the hook and risk baseline before exposing the secret prompt", async () => {
    vi.useFakeTimers();
    secretOperations.current.downloadedFilename = undefined;
    const baseline = deferred<boolean>();
    secretOperations.current.prepareTelemetryBaseline = vi.fn(
      () => baseline.promise,
    );
    secretOperations.current.handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "start" && signal.scope.step === 0) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Secrets policy verified",
          });
        }
        if (signal.type === "start" && signal.scope.step === 1) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Observability plugin downloaded",
          });
        }
      },
    );

    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();
    fireEvent.click(screen.getByRole("button", { name: "Claude Code" }));
    await advanceGuideDelay(3);
    fireEvent.click(
      screen.getByRole("button", { name: "Plugin is installed" }),
    );

    expect(
      secretOperations.current.prepareTelemetryBaseline,
    ).toHaveBeenCalledOnce();
    expect(
      screen.queryByText(/Run this exact command in your shell/),
    ).toBeNull();

    await act(async () => baseline.resolve(true));

    expect(
      screen.getByText(/Run this exact command in your shell/),
    ).toBeTruthy();
  });

  it("shows MCP listening guidance only in Activity", async () => {
    vi.useFakeTimers();
    const waitingMessage =
      "Listening for a new call on the selected governed endpoint";
    const handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "prepare") {
          report({
            type: "success",
            scope: signal.scope,
            result: "Linear mcp server is now setup",
          });
          return;
        }
        if (signal.type === "start" && signal.scope.step < 2) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Step complete",
          });
        }
        if (signal.type === "start" && signal.scope.step === 3) {
          report({
            type: "progress",
            scope: signal.scope,
            message: waitingMessage,
          });
        }
      },
    );
    mcpOperations.current.handleSignal = handleSignal;

    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Server is installed",
      }),
    );
    expect(
      (screen.getByRole("button", { name: "Prompt run" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    mcpOperations.current.promptCopied = true;
    view.rerender(<ProjectGuide />);
    fireEvent.click(screen.getByRole("button", { name: "Prompt run" }));

    const activity = screen.getByRole("log", { name: "Journey A activity" });
    await advanceGuideDelay();
    expect(activity.textContent).toContain(waitingMessage);
    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "waiting",
    );
    expect(screen.queryByRole("link", { name: "Open Tool Logs" })).toBeNull();
  });

  it("shows MCP listening errors only in Activity", async () => {
    vi.useFakeTimers();
    const listenerError =
      "Could not check for the new governed call. Retry after checking the client connection.";
    const handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "prepare") {
          report({
            type: "success",
            scope: signal.scope,
            result: "Linear mcp server is now setup",
          });
          return;
        }
        if (signal.type === "start" && signal.scope.step < 2) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Step complete",
          });
        }
        if (signal.type === "start" && signal.scope.step === 3) {
          report({
            type: "error",
            scope: signal.scope,
            message: listenerError,
          });
        }
      },
    );
    mcpOperations.current.handleSignal = handleSignal;

    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Server is installed",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    mcpOperations.current.promptCopied = true;
    view.rerender(<ProjectGuide />);
    fireEvent.click(screen.getByRole("button", { name: "Prompt run" }));
    await advanceGuideDelay();

    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain(listenerError);
    expect(
      document.querySelector('[aria-current="step"]')?.textContent,
    ).not.toContain(listenerError);
    expect(screen.getByRole("alert").closest('[role="log"]')).toBe(
      screen.getByRole("log", { name: "Journey A activity" }),
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
  });

  it("waits for an agent selection before showing Secret Step 2 as running", async () => {
    vi.useFakeTimers();
    let downloadReport: ((report: ProjectGuideOperationReport) => void) | null =
      null;
    let downloadScope: ProjectGuideOperationScope | null = null;
    secretOperations.current.clientSelected = false;
    secretOperations.current.downloadedFilename = undefined;
    secretOperations.current.setClient = vi.fn(
      (client: "claude" | "cursor" | "codex") => {
        secretOperations.current.client = client;
        secretOperations.current.clientSelected = true;
      },
    );
    secretOperations.current.handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.scope.step === 0) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Secrets policy created",
          });
        }
        if (signal.scope.step === 1) {
          downloadReport = report;
          downloadScope = signal.scope;
        }
      },
    );
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();

    const startButton = screen.getByRole("button", {
      name: "Start the journey",
    });
    expect((startButton as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("alert")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Cursor" }));

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "running",
    );
    expect(screen.queryByText(/Your turn/)).toBeNull();
    expect(downloadReport).not.toBeNull();

    const scope = downloadScope;
    if (!scope) throw new Error("expected download scope");
    act(() => {
      downloadReport?.({
        type: "success",
        scope,
        result: "Observability plugin downloaded",
      });
    });
    await advanceGuideDelay();

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "checkpoint",
    );
    expect(
      screen.getByText(
        "Run the command below to install the observability plugin, then restart your agent so its activity can stream into this project.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/Your turn · Add it to your agent/)).toBeNull();
    expect(
      screen.getByRole("log", { name: "Journey B activity" }).textContent,
    ).toContain("Next · Add it to your agent");
  });

  it("renders an adapter error and retries through the same operation port", async () => {
    vi.useFakeTimers();
    const signals: ProjectGuideOperationSignal[] = [];
    let failStart = true;
    mcpOperations.current.handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        signals.push(signal);
        if (signal.type === "start" && failStart) {
          failStart = false;
          report({
            type: "error",
            scope: signal.scope,
            message: "Catalog unavailable",
          });
        }
      },
    );
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();

    expect(screen.getByRole("alert").textContent).toContain(
      "Catalog unavailable",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "running",
    );
    expect(signals.at(-1)).toMatchObject({
      type: "retry",
      scope: { step: 0, attempt: 1 },
    });
  });

  it("renders supplied current content, output, event, and action callbacks", () => {
    const onPrimaryAction = vi.fn();

    render(
      <ProjectGuideRun
        journey={PROJECT_GUIDE_JOURNEYS[1]!}
        regionId="fixture-run"
        displayState="checkpoint"
        completedSteps={[]}
        currentStep={2}
        currentContent={<p>Coordinator-provided checkpoint</p>}
        output={<span>Coordinator output line</span>}
        eventCard={<section>Coordinator event card</section>}
        primaryAction={{
          label: "Continue run",
          onClick: () => {
            onPrimaryAction();
          },
        }}
        onSwitchJourney={() => undefined}
      />,
    );

    expect(screen.getByText("Coordinator-provided checkpoint")).toBeTruthy();
    expect(screen.getByText("Coordinator output line")).toBeTruthy();
    expect(screen.getByText("Coordinator event card")).toBeTruthy();
    expect(
      screen.getByRole("region", { name: "Govern a third-party MCP" }).id,
    ).toBe("fixture-run");

    fireEvent.click(screen.getByRole("button", { name: "Continue run" }));

    expect(onPrimaryAction).toHaveBeenCalledOnce();
    expect(
      within(
        screen.getByRole("complementary", { name: "Journey A run panel" }),
      ).getAllByRole("button"),
    ).toHaveLength(1);
  });

  it.each([
    {
      journey: PROJECT_GUIDE_JOURNEYS[1]!,
      href: "/projects/request-project/logs",
      label: "Open tool logs",
    },
    {
      journey: PROJECT_GUIDE_JOURNEYS[0]!,
      href: "/projects/request-project/security/events",
      label: "Open Risk Events",
    },
  ])(
    "renders $label as a direct completion link",
    ({ journey, href, label }) => {
      render(
        <ProjectGuideRun
          journey={journey}
          regionId={`${journey.id}-complete-link`}
          displayState="complete"
          completedSteps={journey.steps.map((_, index) => index)}
          currentStep={journey.steps.length}
          currentContent={null}
          output={null}
          eventCard={null}
          primaryAction={{ label, href }}
          onSwitchJourney={() => undefined}
        />,
      );

      const link = screen.getByRole("link", { name: label });
      expect(link.tagName).toBe("A");
      expect(link.closest("button")).toBeNull();
      expect(link.getAttribute("href")).toBe(href);
    },
  );

  it.each([
    {
      journey: PROJECT_GUIDE_JOURNEYS[1]!,
      title: "Govern a third-party MCP",
    },
    {
      journey: PROJECT_GUIDE_JOURNEYS[0]!,
      title: "Block a leaked credential mid-prompt",
    },
  ])(
    "shows Continue when reopening an in-progress $title",
    ({ journey, title }) => {
      statusByJourney.current[journey.id] = "in-progress";
      render(<ProjectGuide />);

      fireEvent.click(screen.getByRole("button", { name: title }));
      fireEvent.click(
        screen.getByRole("button", {
          name: `Rewind to ${journey.steps[0]}`,
        }),
      );

      const action = screen.getByRole("button", { name: "Continue" });
      expect((action as HTMLButtonElement).disabled).toBe(false);
      expect(action.querySelector("svg")).toBeTruthy();
    },
  );

  it("renders the ready start action with the designed play affordance", async () => {
    render(
      <ProjectGuideRun
        journey={PROJECT_GUIDE_JOURNEYS[1]!}
        regionId="ready-run"
        displayState="ready"
        completedSteps={[]}
        currentStep={0}
        currentContent={null}
        output={null}
        eventCard={null}
        primaryAction={{
          label: "Start the journey",
          icon: "play",
          onClick: () => undefined,
        }}
        onSwitchJourney={() => undefined}
      />,
    );

    const action = screen.getByRole("button", { name: "Start the journey" });
    expect((action as HTMLButtonElement).disabled).toBe(false);
    await waitFor(() => expect(action.querySelector("svg")).toBeTruthy());
  });

  it("keeps the ready start action disabled with the play affordance", async () => {
    render(
      <ProjectGuideRun
        journey={PROJECT_GUIDE_JOURNEYS[1]!}
        regionId="disabled-ready-run"
        displayState="ready"
        completedSteps={[]}
        currentStep={0}
        currentContent={null}
        output={null}
        eventCard={null}
        primaryAction={{
          label: "Start the journey",
          icon: "play",
          disabled: true,
        }}
        onSwitchJourney={() => undefined}
      />,
    );

    const action = screen.getByRole("button", { name: "Start the journey" });
    expect((action as HTMLButtonElement).disabled).toBe(true);
    await waitFor(() => expect(action.querySelector("svg")).toBeTruthy());
  });

  it("keeps the installed plugin checkpoint action actionable with a play affordance", () => {
    render(
      <ProjectGuideRun
        journey={PROJECT_GUIDE_JOURNEYS[1]!}
        regionId="checkpoint-run"
        displayState="checkpoint"
        completedSteps={[]}
        currentStep={0}
        currentContent={null}
        output={null}
        eventCard={null}
        primaryAction={{
          label: "Plugin is installed",
          icon: "play",
          disabled: false,
        }}
        onSwitchJourney={() => undefined}
      />,
    );

    const action = screen.getByRole("button", {
      name: "Plugin is installed",
    });
    expect((action as HTMLButtonElement).disabled).toBe(false);
    expect(action.querySelector("svg")).toBeTruthy();
  });

  it("keeps a disabled checkpoint action non-interactive", () => {
    render(
      <ProjectGuideRun
        journey={PROJECT_GUIDE_JOURNEYS[1]!}
        regionId="disabled-checkpoint-run"
        displayState="checkpoint"
        completedSteps={[]}
        currentStep={0}
        currentContent={null}
        output={null}
        eventCard={null}
        primaryAction={{
          label: "Plugin is installed",
          icon: "play",
          disabled: true,
        }}
        onSwitchJourney={() => undefined}
      />,
    );

    const action = screen.getByRole("button", {
      name: "Plugin is installed",
    });
    expect((action as HTMLButtonElement).disabled).toBe(true);
    expect(action.querySelector("svg")).toBeTruthy();
  });

  it("keeps activity history bounded and scrolls to the latest output", () => {
    const journey = PROJECT_GUIDE_JOURNEYS[1]!;
    const { rerender } = render(
      <ProjectGuideRun
        journey={journey}
        regionId="scroll-run"
        displayState="running"
        completedSteps={[]}
        currentStep={0}
        currentContent={null}
        output={<span>First output</span>}
        eventCard={null}
        primaryAction={{ label: "Pause the journey", icon: "pause" }}
        onSwitchJourney={() => undefined}
      />,
    );
    const activity = screen.getByRole("log", { name: "Journey A activity" });
    Object.defineProperty(activity, "scrollHeight", {
      configurable: true,
      value: 400,
    });
    activity.scrollTop = 0;

    rerender(
      <ProjectGuideRun
        journey={journey}
        regionId="scroll-run"
        displayState="running"
        completedSteps={[]}
        currentStep={0}
        currentContent={null}
        output={<span>Latest output</span>}
        eventCard={null}
        primaryAction={{ label: "Pause the journey", icon: "pause" }}
        onSwitchJourney={() => undefined}
      />,
    );

    expect(activity.className).toContain("flex-1");
    expect(activity.className).toContain("overflow-y-auto");
    const stepList = screen.getByRole("list", { name: "Journey A steps" });
    const steps = stepList.querySelectorAll("li");
    expect(steps[0]?.className).toContain("min-h-48");
    for (const step of Array.from(steps).slice(1)) {
      expect(step.className).not.toContain("min-h-48");
    }
    expect(activity.closest("aside")?.className).toContain("min-h-0");
    expect(activity.closest("aside")?.className).not.toContain(
      "overflow-y-auto",
    );
    expect(activity.scrollTop).toBe(400);
  });

  it("keeps listening status in the activity narrative", async () => {
    render(
      <ProjectGuideRun
        journey={PROJECT_GUIDE_JOURNEYS[1]!}
        regionId="waiting-run"
        displayState="waiting"
        completedSteps={[0, 1, 2, 3]}
        currentStep={4}
        currentContent={null}
        output={<span>Listening started</span>}
        eventCard={null}
        primaryAction={{ label: "Pause listening", icon: "pause" }}
        onSwitchJourney={() => undefined}
      />,
    );

    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getByRole("log").textContent).toContain("Listening started");
    expect(
      screen.getByRole("button", { name: "Pause listening" }),
    ).toBeTruthy();
    await waitFor(() =>
      expect(
        screen
          .getByRole("button", { name: "Pause listening" })
          .querySelector("svg"),
      ).toBeTruthy(),
    );
  });

  it("rebuilds the narrative at the artifact resume point", () => {
    statusByJourney.current = {
      "third-party-mcp": "in-progress",
      "secret-block": "not-started",
    };
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(
      screen.getByRole("log", { name: "Journey A activity" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("Server selected");
    expect(
      screen.getByText("Ready · Connect your agent to this server"),
    ).toBeTruthy();
    expect(
      screen.queryByText(
        "Endpoint verified, client connected, no calls recorded.",
      ),
    ).toBeNull();
    expect(screen.queryByText("The call you watched")).toBeNull();
  });

  it("keeps unavailable progress out of an empty or completed display", () => {
    statusByJourney.current = {
      "third-party-mcp": "unreadable",
      "secret-block": "not-started",
    };
    render(<ProjectGuide />);

    expect(screen.getAllByText("Progress unavailable")).toHaveLength(2);
    expect(
      screen.getByTestId("project-guide-third-party-mcp-card").textContent,
    ).not.toContain("Not started");
  });

  it("shows completion actions after both derived journey artifacts are done", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "done",
    };
    render(<ProjectGuide />);

    expect(
      screen.getByRole("heading", {
        name: "Both journeys are on the record.",
      }),
    ).toBeTruthy();
    expect(
      screen.queryByText("This card is replaced on next visit"),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Go to project home" }));

    expect(navigate).toHaveBeenCalledWith("/org/projects/project-guide-test", {
      replace: true,
    });
  });

  it("shows the approved completed-journey summary and keeps the other path available", () => {
    statusByJourney.current = {
      "third-party-mcp": "done",
      "secret-block": "not-started",
    };
    mcpOperations.current.serverName = "Other";
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(
      screen.getByText(
        "Your client now reaches Other through an endpoint you own. Tool lists are filtered to what each caller may use, every call lands in tool logs, and the vendor's server never changed. Remove the server and the path closes.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/reaches linear through/i)).toBeNull();
    expect(screen.getByRole("link", { name: "Open tool logs" })).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Start the other journey" }),
    ).toBeTruthy();
  });

  it("keeps installation disabled while existing project state is pending or unreadable", () => {
    mcpOperations.current.projectStatePending = true;
    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    expect(
      (
        screen.getByRole("button", {
          name: "Start the journey",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);

    mcpOperations.current.projectStatePending = false;
    mcpOperations.current.projectStateError = true;
    view.rerender(<ProjectGuide />);

    expect(
      (
        screen.getByRole("button", {
          name: "Start the journey",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("shows the secret policy phases instead of client tabs on step one", () => {
    render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );

    expect(screen.getByText("What the wizard does")).toBeTruthy();
    expect(screen.getByText("Enable the Secrets category")).toBeTruthy();
    expect(screen.getByText("Scope user prompts")).toBeTruthy();
    expect(screen.getByText("Deny the request")).toBeTruthy();
    const phaseStatuses = screen.getAllByText("not run");
    expect(phaseStatuses).toHaveLength(3);
    for (const phaseStatus of phaseStatuses) {
      expect(phaseStatus.className).toContain("text-eyebrow");
      expect(phaseStatus.className).toContain("text-disabled");
      expect(phaseStatus.className).toContain("ml-auto");
    }
    expect(screen.queryByRole("tab", { name: "Claude Code" })).toBeNull();
    expect(screen.queryByRole("tab", { name: "Cursor" })).toBeNull();
    expect(screen.queryByRole("tab", { name: "Codex" })).toBeNull();
    expect(
      screen.queryByText(
        /The next step downloads the existing observability ZIP/,
      ),
    ).toBeNull();
  });

  it("starts plugin setup when Step 2 selects an agent", () => {
    statusByJourney.current["secret-block"] = "in-progress";
    secretOperations.current.downloadedFilename = undefined;
    secretOperations.current.clientSelected = false;
    const handleSignal = vi.fn();
    secretOperations.current.handleSignal = handleSignal;
    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );

    expect(
      screen.getByRole("group", { name: "Choose your agent" }),
    ).toBeTruthy();
    const picker = screen.getByRole("group", { name: "Choose your agent" });
    const options = picker.querySelector('[class*="overflow-x-auto"]');
    expect(options?.className).toContain("flex");
    expect(options?.className).toContain("overflow-x-auto");
    expect(
      screen.getByRole("button", { name: "Start the journey" }),
    ).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(handleSignal).not.toHaveBeenCalled();
    for (const agent of ["Claude Code", "Cursor", "Codex"]) {
      expect(
        screen
          .getByRole("button", { name: agent })
          .getAttribute("aria-pressed"),
      ).toBe("false");
    }
    expect(screen.getByText("What you get")).toBeTruthy();
    expect(screen.getByText("Build the plugin for this project")).toBeTruthy();
    expect(screen.getByText("Sign the bundle")).toBeTruthy();
    expect(screen.getAllByText("not run")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "Codex" }));
    expect(secretOperations.current.setClient).toHaveBeenCalledWith("codex");
    secretOperations.current.clientSelected = true;
    secretOperations.current.client = "codex";
    view.rerender(<ProjectGuide />);
    expect(
      screen
        .getByRole("button", { name: "Codex" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
    expect(
      screen.getByRole("button", { name: "Pause the journey" }),
    ).toBeTruthy();
    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "running",
    );
    expect(handleSignal).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "start",
        scope: expect.objectContaining({ path: "secret-block", step: 1 }),
      }),
      expect.any(Function),
    );
  });

  it("shows the plugin setup title while the agent is being selected", () => {
    statusByJourney.current["secret-block"] = "in-progress";
    secretOperations.current.downloadedFilename = "gram-observability.zip";
    secretOperations.current.promptCopied = false;
    render(<ProjectGuide />);

    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );
    expect(
      screen.getByRole("heading", { name: "Setup the observability plugin" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Start the journey" }),
    ).toBeTruthy();
  });

  it("renders the real MCP selection, connection, prompt, and observed call", async () => {
    vi.useFakeTimers();
    const handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "prepare") {
          report({
            type: "success",
            scope: signal.scope,
            result: "Linear mcp server is now setup",
          });
        }
        if (signal.type === "start" && signal.scope.step === 0) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Linear installed as a governed MCP server",
          });
        }
        if (signal.type === "start" && signal.scope.step === 1) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Linear is ready on its governed endpoint",
          });
        }
        if (signal.type === "start" && signal.scope.step === 3) {
          report({
            type: "event",
            scope: signal.scope,
            event: {
              kind: "Governed call",
              tone: "allow",
              title: "Linear",
              rows: [
                { key: "server", value: "Linear" },
                { key: "calls", value: "1 recorded" },
              ],
              note: "The new call is recorded in Tool Logs.",
            },
          });
        }
      },
    );
    mcpOperations.current.handleSignal = handleSignal;

    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );

    fireEvent.click(screen.getByRole("button", { name: /Linear.*2 tools/ }));
    expect(mcpOperations.current.selectServer).toHaveBeenCalledWith(
      catalogServer,
    );
    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain("Linear selected. Ready to start the journey");
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();

    expect(screen.getByRole("tab", { name: "Claude" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Cursor" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Codex" })).toBeTruthy();
    expect(
      screen.getByText((_, element) => {
        if (element?.tagName !== "PRE") return false;
        return Boolean(
          element.textContent?.includes(
            "claude mcp add --transport http --scope user 'Linear_Governed'",
          ),
        );
      }),
    ).toBeTruthy();
    expect(
      screen.queryByText("https://api.example/mcp/linear-endpoint"),
    ).toBeNull();
    expect(
      screen.queryByRole("link", { name: "View Linear MCP server" }),
    ).toBeNull();

    expect(
      screen
        .getByRole("button", { name: "Server is installed" })
        .querySelector("svg"),
    ).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Server is installed",
      }),
    );
    await advanceGuideDelay();
    expect(
      screen.getByText((_, element) => {
        if (element?.tagName !== "PRE") return false;
        return Boolean(
          element.textContent?.includes("first list the available tools") &&
          element.textContent.includes(
            "Do not create, update, or delete anything",
          ),
        );
      }),
    ).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Prompt run" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      screen.getByRole("button", { name: "Prompt run" }).querySelector("svg"),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    mcpOperations.current.promptCopied = true;
    view.rerender(<ProjectGuide />);
    expect(
      (screen.getByRole("button", { name: "Prompt run" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Prompt run" }));
    await advanceGuideDelay();
    expect(
      screen.getByRole("heading", { name: "The path is governed." }),
    ).toBeTruthy();

    expect(
      screen.getByRole("heading", { name: "The path is governed." }),
    ).toBeTruthy();
    expect(screen.getByText("1 recorded")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Open tool logs" }).getAttribute("href"),
    ).toBe("/projects/request-project/logs");
    expect(
      screen.getByRole("button", { name: "Start the other journey" }),
    ).toBeTruthy();
  });

  it("renders the real blocked-secret install, prompt checkpoint, event, and Risk Events link", async () => {
    vi.useFakeTimers();
    secretOperations.current.downloadedFilename = undefined;
    const handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "start" && signal.scope.step === 0) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Secrets policy already live · block on match",
          });
        }
        if (signal.type === "start" && signal.scope.step === 1) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Observability plugin downloaded · gram-observability.zip",
          });
        }
        if (signal.type === "start" && signal.scope.step === 4) {
          report({
            type: "event",
            scope: signal.scope,
            event: {
              kind: "Denied · risk event",
              tone: "deny",
              title: "request denied by secrets policy",
              rows: [
                { key: "rule", value: "secrets.aws_access_key_id" },
                { key: "match", value: "synthetic credential" },
              ],
              note: "The prompt was blocked before the model answered.",
            },
          });
        }
      },
    );
    secretOperations.current.handleSignal = handleSignal;

    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );

    expect(screen.getByText("What the wizard does")).toBeTruthy();
    expect(screen.getByText("Enable the Secrets category")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();
    fireEvent.click(screen.getByRole("button", { name: "Claude Code" }));
    await advanceGuideDelay(3);

    const installPre = screen.getByText((_, element) =>
      Boolean(
        element?.tagName === "PRE" &&
        element.textContent?.includes(
          "unzip -oq gram-observability.zip -d ~/.claude/plugins/",
        ),
      ),
    );
    expect(installPre?.className).toContain("min-w-0");
    expect(installPre?.className).toContain("max-w-full");
    expect(installPre?.className).toContain("w-full");
    expect(installPre?.className).toContain("whitespace-pre-wrap");
    expect(installPre?.closest(".snippet-inner")?.className).toContain(
      "min-w-0",
    );
    const currentStepItem = installPre?.closest("li");
    expect(currentStepItem?.className).toContain("min-w-0");
    expect(
      currentStepItem?.querySelector(":scope > div.grid.min-w-0"),
    ).toBeTruthy();
    expect(installPre?.closest(".snippet")?.parentElement?.className).toContain(
      "min-w-0",
    );
    expect(
      screen.getByText(
        "Run the command below to install the observability plugin, then restart your agent so its activity can stream into this project.",
      ),
    ).toBeTruthy();
    expect(
      screen.queryByText(
        /launches Claude Code with the extracted plugin active/i,
      ),
    ).toBeNull();
    expect(screen.queryByText(/Your turn · Add it to your agent/)).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Plugin is installed" }),
    );
    await advanceGuideDelay();
    const promptPre = screen.getByText((_, element) =>
      Boolean(
        element?.tagName === "PRE" &&
        element.textContent?.match(/Run this exact command in your shell/),
      ),
    );
    expect(promptPre.className).toContain("min-w-0");
    expect(promptPre.className).toContain("max-w-full");
    expect(promptPre.className).toContain("w-full");
    expect(promptPre.className).toContain("whitespace-pre-wrap");
    expect(promptPre.closest(".snippet-inner")?.className).toContain("min-w-0");
    expect(promptPre.closest(".snippet")?.parentElement?.className).toContain(
      "min-w-0",
    );
    expect(
      screen.getByText(/Run this exact command in your shell/),
    ).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Prompt run" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    secretOperations.current.promptCopied = true;
    view.rerender(<ProjectGuide />);
    fireEvent.click(screen.getByRole("button", { name: "Prompt run" }));
    await advanceGuideDelay();

    expect(
      screen.getByRole("heading", { name: "The prompt was denied." }),
    ).toBeTruthy();
    expect(screen.getByText("secrets.aws_access_key_id")).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Open Risk Events" })
        .getAttribute("href"),
    ).toBe("/projects/request-project/security/events");
    expect(
      screen
        .getByRole("link", { name: "View risk events" })
        .getAttribute("href"),
    ).toBe("/projects/request-project/security/events");
  });

  it("keeps the secret prompt visible and reports send failures in the activity log", async () => {
    vi.useFakeTimers();
    secretOperations.current.downloadedFilename = undefined;
    const listenerError =
      "Could not check for the blocked event after the prompt was copied. Retry the step.";

    const handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "prepare") {
          report({
            type: "success",
            scope: signal.scope,
            result: "Linear mcp server is now setup",
          });
          return;
        }
        if (signal.type === "start" && signal.scope.step < 2) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Step complete",
          });
        }
        if (signal.type === "start" && signal.scope.step === 4) {
          report({
            type: "error",
            scope: signal.scope,
            message: listenerError,
          });
        }
      },
    );
    secretOperations.current.handleSignal = handleSignal;

    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();
    fireEvent.click(screen.getByRole("button", { name: "Claude Code" }));
    await advanceGuideDelay(3);
    fireEvent.click(
      screen.getByRole("button", { name: "Plugin is installed" }),
    );
    await advanceGuideDelay();

    const promptPre = screen.getByText((_, element) =>
      Boolean(
        element?.tagName === "PRE" &&
        element.textContent?.includes("Run this exact command in your shell"),
      ),
    );
    expect(promptPre).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Retry baseline" })).toBeNull();
    expect(
      (screen.getByRole("button", { name: "Prompt run" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    secretOperations.current.promptCopied = true;
    view.rerender(<ProjectGuide />);
    fireEvent.click(screen.getByRole("button", { name: "Prompt run" }));
    await advanceGuideDelay();
    expect(
      screen.getByRole("log", { name: "Journey B activity" }).textContent,
    ).toContain("Prompt copied · listening for the blocked event");
    expect(
      document.querySelector('[aria-current="step"]')?.textContent,
    ).not.toContain(listenerError);
    expect(
      screen.queryByText(
        "Could not capture the hook and risk-event baseline. Retry it before copying the prompt.",
      ),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
  });

  it("shows secret listening guidance only in activity before an event exists", async () => {
    vi.useFakeTimers();
    secretOperations.current.downloadedFilename = undefined;
    const handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "start" && signal.scope.step < 2) {
          report({
            type: "success",
            scope: signal.scope,
            result: "Step complete",
          });
        }
        if (signal.type === "start" && signal.scope.step === 4) {
          report({
            type: "progress",
            scope: signal.scope,
            message:
              "Waiting for a new blocked hook and matching secrets risk event.",
          });
        }
      },
    );
    secretOperations.current.handleSignal = handleSignal;

    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();
    fireEvent.click(screen.getByRole("button", { name: "Claude Code" }));
    await advanceGuideDelay(3);
    fireEvent.click(
      screen.getByRole("button", { name: "Plugin is installed" }),
    );
    await advanceGuideDelay();
    const promptPre = screen.getByText((_, element) =>
      Boolean(
        element?.tagName === "PRE" &&
        element.textContent?.includes("Run this exact command in your shell"),
      ),
    );
    expect(promptPre).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    secretOperations.current.promptCopied = true;
    view.rerender(<ProjectGuide />);
    fireEvent.click(screen.getByRole("button", { name: "Prompt run" }));
    await advanceGuideDelay();
    const activity = screen.getByRole("log", { name: "Journey B activity" });
    expect(activity.textContent).toContain(
      "Waiting for a new blocked hook and matching secrets risk event.",
    );
    expect(screen.getByTestId("project-guide-run").dataset.displayState).toBe(
      "waiting",
    );
    expect(screen.queryByRole("link", { name: "Open Risk Events" })).toBeNull();
  });

  it("times out and retries the blocked-secret listener without accepting history", async () => {
    vi.useFakeTimers();
    secretOperations.current.downloadedFilename = undefined;
    const handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "start" && signal.scope.step < 2) {
          report({
            type: "success",
            scope: signal.scope,
            result:
              signal.scope.step === 0
                ? "Secrets policy live"
                : "Observability plugin downloaded",
          });
        }
      },
    );
    secretOperations.current.handleSignal = handleSignal;

    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", {
        name: /Block a leaked credential mid-prompt/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();
    fireEvent.click(screen.getByRole("button", { name: "Claude Code" }));
    await advanceGuideDelay(3);
    fireEvent.click(
      screen.getByRole("button", { name: "Plugin is installed" }),
    );
    await advanceGuideDelay();
    expect(screen.queryByRole("button", { name: "Sent it" })).toBeNull();
    const promptPre = screen.getByText((_, element) =>
      Boolean(
        element?.tagName === "PRE" &&
        element.textContent?.includes("Run this exact command in your shell"),
      ),
    );
    expect(promptPre).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    secretOperations.current.promptCopied = true;
    view.rerender(<ProjectGuide />);
    fireEvent.click(screen.getByRole("button", { name: "Prompt run" }));

    act(() => {
      vi.advanceTimersByTime(LISTEN_TIMEOUT_SECONDS * 1_000);
    });

    expect(
      screen.getByRole("log", { name: "Journey B activity" }).textContent,
    ).toContain(
      `No event seen in ${LISTEN_TIMEOUT_SECONDS}s. Check the client, then listen again.`,
    );
    expect(screen.getByRole("alert").closest('[role="log"]')).toBe(
      screen.getByRole("log", { name: "Journey B activity" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(handleSignal).toHaveBeenLastCalledWith(
      {
        type: "retry",
        scope: {
          path: "secret-block",
          step: 4,
          attempt: 1,
          runId: 3,
        },
      },
      expect.any(Function),
    );
  });

  it("times out without a new governed call and retries the same listening step", async () => {
    vi.useFakeTimers();
    const handleSignal = vi.fn(
      (
        signal: ProjectGuideOperationSignal,
        report: (report: ProjectGuideOperationReport) => void,
      ) => {
        if (signal.type === "prepare") {
          report({
            type: "success",
            scope: signal.scope,
            result: "Linear mcp server is now setup",
          });
          return;
        }
        if (signal.type === "start" && signal.scope.step < 2) {
          report({
            type: "success",
            scope: signal.scope,
            result:
              signal.scope.step === 0 ? "Server installed" : "Endpoint ready",
          });
        }
      },
    );
    mcpOperations.current.handleSignal = handleSignal;

    const view = render(<ProjectGuide />);
    fireEvent.click(
      screen.getByRole("button", { name: /Govern a third-party MCP/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start the journey" }));
    await advanceGuideDelay();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Server is installed",
      }),
    );
    await advanceGuideDelay();
    fireEvent.click(screen.getByRole("button", { name: "copy" }));
    mcpOperations.current.promptCopied = true;
    view.rerender(<ProjectGuide />);
    fireEvent.click(screen.getByRole("button", { name: "Prompt run" }));

    act(() => {
      vi.advanceTimersByTime(LISTEN_TIMEOUT_SECONDS * 1_000);
    });

    expect(
      screen.getByRole("log", { name: "Journey A activity" }).textContent,
    ).toContain(
      `No event seen in ${LISTEN_TIMEOUT_SECONDS}s. Check the client, then listen again.`,
    );
    expect(screen.getByRole("alert").closest('[role="log"]')).toBe(
      screen.getByRole("log", { name: "Journey A activity" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(handleSignal).toHaveBeenLastCalledWith(
      {
        type: "retry",
        scope: {
          path: "third-party-mcp",
          step: 3,
          attempt: 1,
          runId: 2,
        },
      },
      expect.any(Function),
    );
    expect(screen.queryByRole("status")).toBeNull();
  });
});
