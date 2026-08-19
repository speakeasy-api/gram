import {
  cleanup,
  fireEvent,
  render as baseRender,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import { MemoryRouter } from "react-router";

const queries = vi.hoisted(() => ({
  policies: vi.fn(),
  results: vi.fn(),
  traces: vi.fn(),
}));
const mutation = vi.hoisted(() => ({ mutateAsync: vi.fn() }));
const fetcher = vi.hoisted(() => ({ fetch: vi.fn() }));
const downloads = vi.hoisted(() => vi.fn());
const invalidations = vi.hoisted(() => ({
  allPolicies: vi.fn(),
  projectPolicies: vi.fn(),
}));
const revokeObjectURL = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-guide-test",
}));

vi.mock("@/contexts/Fetcher", () => ({ useFetcher: () => fetcher }));

vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  invalidateAllRiskListPolicies: invalidations.allPolicies,
  invalidateRiskListPolicies: invalidations.projectPolicies,
  useRiskListPolicies: queries.policies,
}));

vi.mock("@gram/client/react-query/riskCreatePolicy.js", () => ({
  useRiskCreatePolicyMutation: () => mutation,
}));

vi.mock("@gram/client/react-query/listHooksTraces.js", () => ({
  useListHooksTraces: queries.traces,
}));

vi.mock("@gram/client/react-query/riskListResults.js", () => ({
  useRiskListResults: queries.results,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    riskEvents: {
      href: () => "/organization/projects/project/risk-events",
      Link: ({ children }: { children: React.ReactNode }) => (
        <a href="/organization/projects/project/risk-events">{children}</a>
      ),
    },
  }),
}));

import { SecretBlockJourney } from "./SecretBlockJourney";

function render(ui: React.ReactNode) {
  return baseRender(ui, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={new QueryClient()}>
        <ConfigProvider theme="light" setTheme={() => {}}>
          <MemoryRouter>{children}</MemoryRouter>
        </ConfigProvider>
      </QueryClientProvider>
    ),
  });
}

function blockingPolicy() {
  return {
    action: "block",
    audiencePrincipalUrns: ["user:all"],
    audienceType: "everyone",
    autoName: true,
    createdAt: new Date(),
    enabled: true,
    id: "policy-test",
    name: "Secrets policy",
    policyType: "standard",
    projectId: "project-test",
    score: 5,
    sources: ["gitleaks"],
    updatedAt: new Date(),
    version: 1,
  };
}

function hookTrace() {
  return {
    gramUrn: "urn:gram:trace:test",
    logCount: 1,
    startTimeUnixNano: "1",
    traceId: "00000000000000000000000000000000",
  };
}

function secretResult(overrides = {}) {
  return {
    createdAt: new Date("2026-08-19T12:00:00Z"),
    id: "result-test",
    policyId: "policy-test",
    policyVersion: 1,
    ruleId: "journey_test_rule",
    source: "gitleaks",
    ...overrides,
  };
}

beforeEach(() => {
  mutation.mutateAsync.mockReset().mockResolvedValue(blockingPolicy());
  fetcher.fetch.mockReset().mockResolvedValue(new Response("zip"));
  downloads.mockReset();
  invalidations.allPolicies.mockReset().mockResolvedValue(undefined);
  invalidations.projectPolicies.mockReset().mockResolvedValue(undefined);
  revokeObjectURL.mockReset();
  queries.policies.mockReset().mockReturnValue({
    data: { policies: [] },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  });
  queries.traces.mockReset().mockReturnValue({
    data: { traces: [] },
    isError: false,
    isPending: false,
  });
  queries.results.mockReset().mockReturnValue({
    data: { results: [], totalCount: 0 },
    isError: false,
    isPending: false,
  });
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(
    function (this: HTMLAnchorElement) {
      downloads(this.download);
    },
  );
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:test");
  vi.spyOn(URL, "revokeObjectURL").mockImplementation((url) => {
    revokeObjectURL(url);
  });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("SecretBlockJourney", () => {
  it("renders the approved five-step run with an activity panel", () => {
    render(
      <SecretBlockJourney
        status="not-started"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    const steps = screen.getByRole("list", { name: "Journey B steps" });
    expect(steps.querySelectorAll(":scope > li")).toHaveLength(5);
    expect(
      screen.getByText("Create a secrets policy set to deny"),
    ).toBeTruthy();
    expect(screen.getByText("Download the observability plugin")).toBeTruthy();
    expect(screen.getByText("Add it to your agent")).toBeTruthy();
    expect(
      screen.getByText("Send a prompt with a synthetic secret"),
    ).toBeTruthy();
    expect(screen.getByText("Watch the block land")).toBeTruthy();
    expect(
      screen.getByRole("log", { name: "Journey B activity" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Start the journey" }),
    ).toBeTruthy();
  });

  it("skips creation when a matching blocking secrets policy already exists", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <SecretBlockJourney
        status="not-started"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(
      screen.getByRole("heading", {
        name: "Download the observability plugin",
      }),
    ).toBeTruthy();
    expect(mutation.mutateAsync).not.toHaveBeenCalled();
  });

  it("creates the blocking secrets policy with the required request", async () => {
    render(
      <SecretBlockJourney
        status="not-started"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Publish policy" }));

    await waitFor(() =>
      expect(mutation.mutateAsync).toHaveBeenCalledWith({
        request: {
          gramProject: "project-guide-test",
          createRiskPolicyRequestBody: {
            enabled: true,
            action: "block",
            sources: ["gitleaks"],
            messageTypes: ["tool_request", "tool_response"],
            audienceType: "everyone",
            policyType: "standard",
            autoName: true,
          },
        },
      }),
    );
    await waitFor(() =>
      expect(invalidations.projectPolicies).toHaveBeenCalledWith(
        expect.any(QueryClient),
        [{ gramProject: "project-guide-test" }],
      ),
    );
    expect(invalidations.allPolicies).not.toHaveBeenCalled();
  });

  it("keeps policy creation retryable after an error", async () => {
    mutation.mutateAsync.mockRejectedValueOnce(new Error("unavailable"));
    render(
      <SecretBlockJourney
        status="not-started"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Publish policy" }));

    await waitFor(() =>
      expect(
        screen.getByText("Could not publish the secrets policy."),
      ).toBeTruthy(),
    );
    expect(screen.getByRole("button", { name: "Publish policy" })).toBeTruthy();
    expect(
      screen.queryByRole("heading", {
        name: "Install the observability plugin",
      }),
    ).toBeNull();
  });

  it("downloads ZIPs only for supported client platforms and reports failures", async () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    fetcher.fetch
      .mockResolvedValueOnce(
        new Response("zip", {
          headers: {
            "Content-Disposition": 'attachment; filename="project-hooks.zip"',
          },
        }),
      )
      .mockResolvedValueOnce(new Response("zip"))
      .mockResolvedValueOnce(new Response("nope", { status: 500 }));
    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(
      screen.getByRole("heading", {
        name: "Download the observability plugin",
      }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Download ZIP" }));
    await waitFor(() =>
      expect(fetcher.fetch).toHaveBeenCalledWith(
        "/rpc/plugins.downloadObservabilityPlugin?platform=claude",
        {},
      ),
    );
    expect(downloads).toHaveBeenCalledWith("project-hooks.zip");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:test");
    expect(
      screen.getByRole("heading", { name: "Add it to your agent" }),
    ).toBeTruthy();
    expect(screen.getByText(/unzip project-hooks\.zip/)).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "Download another ZIP" }),
    );
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Cursor" }), {
      button: 0,
    });
    await waitFor(() =>
      expect(
        screen.getByRole("tab", { name: "Cursor" }).getAttribute("data-state"),
      ).toBe("active"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Download ZIP" }));
    await waitFor(() =>
      expect(fetcher.fetch).toHaveBeenCalledWith(
        "/rpc/plugins.downloadObservabilityPlugin?platform=cursor",
        {},
      ),
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Download another ZIP" }),
    );
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Codex" }), {
      button: 0,
    });
    await waitFor(() =>
      expect(
        screen.getByRole("tab", { name: "Codex" }).getAttribute("data-state"),
      ).toBe("active"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Download ZIP" }));

    await waitFor(() =>
      expect(
        screen.getByText("Failed to download observability plugin."),
      ).toBeTruthy(),
    );
  });

  it("uses Radix client tabs with a panel", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(
      screen
        .getByRole("tab", { name: "Claude Code" })
        .getAttribute("data-state"),
    ).toBe("active");
    expect(screen.getByRole("tabpanel")).toBeTruthy();
  });

  it("explains installation without claiming a download proves it", async () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Download ZIP" }));

    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Add it to your agent" }),
      ).toBeTruthy(),
    );
    expect(screen.getByText(/Extract the ZIP in your agent/)).toBeTruthy();
    expect(screen.getByText(/Waiting for the first hook event/)).toBeTruthy();
    expect(screen.queryByText(/download confirms installation/i)).toBeNull();
  });

  it("resumes after reload when the policy and a hook trace already exist", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: {
        traces: [hookTrace()],
      },
      isError: false,
      isPending: false,
    });
    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(
      screen.getByText("Plugin connected. Send the synthetic secret prompt."),
    ).toBeTruthy();
    expect(screen.getByText(/AKIAIOSFODNN7EXAMPLE/)).toBeTruthy();
  });

  it("queries hook traces for this project with the bounded installation window", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: { traces: [hookTrace()] },
      isError: false,
      isPending: false,
    });

    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(queries.traces).toHaveBeenCalledWith(
      {
        gramProject: "project-guide-test",
        listHooksTracesPayload: {
          from: new Date(0),
          to: expect.any(Date),
          typesToInclude: ["mcp", "local", "skill"],
          limit: 10,
          sort: "desc",
        },
      },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
    const traceOptions = queries.traces.mock.calls[0]?.[2];
    expect(
      traceOptions.refetchInterval({
        state: { data: { traces: [hookTrace()] } },
      }),
    ).toBe(false);
  });

  it("advances the hook-query upper bound before every refetch", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-19T12:00:00Z"));
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    try {
      render(
        <SecretBlockJourney
          status="in-progress"
          onComplete={() => {}}
          onSwitchJourney={() => {}}
        />,
      );

      const request = queries.traces.mock.calls[0]?.[0];
      const traceOptions = queries.traces.mock.calls[0]?.[2];
      expect(request.listHooksTracesPayload.to).toEqual(
        new Date("2026-08-19T12:00:00Z"),
      );

      vi.setSystemTime(new Date("2026-08-19T12:00:05Z"));
      expect(
        traceOptions.refetchInterval({ state: { data: { traces: [] } } }),
      ).toBe(2_000);
      expect(request.listHooksTracesPayload.to).toEqual(
        new Date("2026-08-19T12:00:05Z"),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not treat a failed hook query as an installed plugin", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: { traces: [hookTrace()] },
      isError: true,
      isPending: false,
    });

    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(
      screen.getByRole("heading", {
        name: "Download the observability plugin",
      }),
    ).toBeTruthy();
    expect(screen.queryByText(/Plugin connected/)).toBeNull();
  });

  it("names the selected client beside the safe synthetic-secret prompt", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    const view = render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );
    fireEvent.mouseDown(screen.getByRole("tab", { name: "Cursor" }), {
      button: 0,
    });
    queries.traces.mockReturnValue({
      data: { traces: [hookTrace()] },
      isError: false,
      isPending: false,
    });

    view.rerender(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(screen.getByText(/Copy this into Cursor/)).toBeTruthy();
    expect(screen.getByText(/AKIAIOSFODNN7EXAMPLE/)).toBeTruthy();
  });

  it("renders the newest blocked risk result without its raw match", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: { traces: [hookTrace()] },
      isError: false,
      isPending: false,
    });
    queries.results.mockReturnValue({
      data: {
        results: [
          secretResult({ createdAt: new Date("2026-08-19T11:00:00Z") }),
          secretResult({
            createdAt: new Date("2026-08-19T12:00:00Z"),
            description: "The request was blocked before the model answered.",
            match: "must-not-render",
          }),
        ],
        totalCount: 2,
      },
      isError: false,
      isPending: false,
    });

    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(screen.getByText("Journey Test Rule")).toBeTruthy();
    expect(
      screen.getByText("The request was blocked before the model answered."),
    ).toBeTruthy();
    expect(screen.getByText("Blocked by secrets policy")).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Open Risk Events" })
        .getAttribute("href"),
    ).toBe("/organization/projects/project/risk-events");
    expect(screen.queryByText("must-not-render")).toBeNull();
    expect(queries.results).toHaveBeenCalledWith(
      { gramProject: "project-guide-test", category: "secrets", limit: 10 },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
    const resultOptions = queries.results.mock.calls[0]?.[2];
    expect(
      resultOptions.refetchInterval({
        state: { data: { results: [secretResult()] } },
      }),
    ).toBe(false);
  });

  it("credits a persisted risk result and does not complete without one", async () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: { traces: [hookTrace()] },
      isError: false,
      isPending: false,
    });
    const onComplete = vi.fn();
    const view = render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={() => {}}
      />,
    );

    expect(onComplete).not.toHaveBeenCalled();

    queries.results.mockReturnValue({
      data: { results: [secretResult()], totalCount: 1 },
      isError: false,
      isPending: false,
    });
    view.rerender(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={() => {}}
      />,
    );

    await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));
  });

  it("does not complete from done status when scoped risk results are empty", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: { traces: [hookTrace()] },
      isError: false,
      isPending: false,
    });
    const onComplete = vi.fn();

    render(
      <SecretBlockJourney
        status="done"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={() => {}}
      />,
    );

    expect(onComplete).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Copy prompt" }));
    fireEvent.click(screen.getByRole("button", { name: "Sent it" }));
    expect(screen.getByRole("status").textContent).toContain(
      "Listening for risk events on this project",
    );
    expect(screen.queryByText("Journey B complete")).toBeNull();
  });

  it("does not complete from done status when scoped risk results error", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: { traces: [hookTrace()] },
      isError: false,
      isPending: false,
    });
    queries.results.mockReturnValue({
      data: undefined,
      isError: true,
      isPending: false,
      refetch: vi.fn(),
    });
    const onComplete = vi.fn();

    render(
      <SecretBlockJourney
        status="done"
        onComplete={() => {
          onComplete();
        }}
        onSwitchJourney={() => {}}
      />,
    );

    expect(onComplete).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Copy prompt" }));
    fireEvent.click(screen.getByRole("button", { name: "Sent it" }));
    expect(
      screen.getByText("Could not check for blocked risk events."),
    ).toBeTruthy();
    expect(screen.queryByText("Journey B complete")).toBeNull();
  });

  it("shows the risk-event listener after hook activity until a result arrives", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: { traces: [hookTrace()] },
      isError: false,
      isPending: false,
    });

    render(
      <SecretBlockJourney
        status="in-progress"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Copy prompt" }));
    fireEvent.click(screen.getByRole("button", { name: "Sent it" }));
    expect(screen.getByRole("status").textContent).toContain(
      "Listening for risk events on this project",
    );
    expect(screen.getByText("Waiting for a blocked risk event")).toBeTruthy();
  });

  it("renders the denied event in the completion summary", () => {
    queries.policies.mockReturnValue({
      data: { policies: [blockingPolicy()] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    queries.traces.mockReturnValue({
      data: { traces: [hookTrace()] },
      isError: false,
      isPending: false,
    });
    queries.results.mockReturnValue({
      data: {
        results: [
          secretResult({
            description: "The request was blocked before the model answered.",
            match: "must-not-render",
          }),
        ],
        totalCount: 1,
      },
      isError: false,
      isPending: false,
    });

    render(
      <SecretBlockJourney
        status="done"
        onComplete={() => {}}
        onSwitchJourney={() => {}}
      />,
    );

    expect(screen.getByText("Journey B complete")).toBeTruthy();
    expect(screen.getByText("The event you watched")).toBeTruthy();
    expect(screen.getByText("Denied · risk event")).toBeTruthy();
    expect(screen.queryByText("must-not-render")).toBeNull();
  });
});
