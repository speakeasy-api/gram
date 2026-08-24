import { act, renderHook, waitFor } from "@testing-library/react";
import type { HookTraceSummary } from "@gram/client/models/components/hooktracesummary.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectGuideOperationReport } from "./projectGuideMachine";

const queryHooks = vi.hoisted(() => ({
  hooks: vi.fn(),
  policies: vi.fn(),
  results: vi.fn(),
}));
const mutateAsync = vi.hoisted(() => vi.fn());
const authFetch = vi.hoisted(() => vi.fn());
const downloadResponse = vi.hoisted(() => vi.fn(() => Promise.resolve()));
const invalidatePolicies = vi.hoisted(() => vi.fn(() => Promise.resolve()));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "request-project",
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "organization-id" }),
}));
vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: authFetch }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    riskEvents: { href: () => "/projects/request-project/security/events" },
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
vi.mock("@gram/client/react-query/riskListPolicies.js", () => ({
  invalidateRiskListPolicies: invalidatePolicies,
  useRiskListPolicies: queryHooks.policies,
}));
vi.mock("@gram/client/react-query/riskCreatePolicy.js", () => ({
  useRiskCreatePolicyMutation: () => ({ mutateAsync }),
}));
vi.mock("@gram/client/react-query/listHooksTraces.js", () => ({
  useListHooksTraces: queryHooks.hooks,
}));
vi.mock("@gram/client/react-query/riskListResults.js", () => ({
  useRiskListResults: queryHooks.results,
}));
vi.mock("@/pages/plugins/downloadPluginPackage", () => ({
  downloadResponse,
}));
vi.mock("@/pages/security/risk-utils", () => ({
  getRuleTitleFallback: (ruleId?: string) => ruleId ?? "Secrets rule",
}));

import { useSecretGuideOperations } from "./useSecretGuideOperations";

const POLICY_SCOPE = {
  path: "secret-block" as const,
  step: 0,
  attempt: 0,
  runId: 1,
};

const SYNTHETIC_SECRET_REDACTION = "<redacted len=37 sha=61966c81>";

function policy(overrides: Partial<RiskPolicy> = {}): RiskPolicy {
  return {
    action: "block",
    audiencePrincipalUrns: ["user:all"],
    audienceType: "everyone",
    autoName: true,
    createdAt: new Date("2026-08-19T12:00:00Z"),
    enabled: true,
    id: "secrets-policy",
    messageTypes: ["tool_request", "tool_response"],
    name: "Secrets",
    policyType: "standard",
    projectId: "project-id",
    score: 5,
    sources: ["gitleaks"],
    updatedAt: new Date("2026-08-19T12:00:00Z"),
    version: 1,
    ...overrides,
  };
}

function trace(overrides: Partial<HookTraceSummary> = {}): HookTraceSummary {
  return {
    blockReason:
      'Speakeasy blocked this tool call: matched policy "Secrets" (synthetic credential)',
    gramUrn: "urn:gram:hook",
    hookSource: "claude-code",
    hookStatus: "blocked",
    logCount: 1,
    startTimeUnixNano: "1787140800000000000",
    traceId: "old-trace",
    ...overrides,
  };
}

function result(overrides: Partial<RiskResult> = {}): RiskResult {
  return {
    createdAt: new Date("2026-08-19T12:00:00Z"),
    id: "old-result",
    policyId: "secrets-policy",
    policyVersion: 1,
    ruleId: "secrets.aws_access_key_id",
    source: "gitleaks",
    ...overrides,
  };
}

function queryResult<T>(data: T) {
  return {
    data,
    isError: false,
    isFetching: false,
    isPending: false,
    refetch: vi.fn(() => Promise.resolve({ data, isError: false })),
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  queryHooks.policies.mockReturnValue(queryResult({ policies: [policy()] }));
  queryHooks.hooks.mockReturnValue(queryResult({ traces: [] }));
  queryHooks.results.mockReturnValue(queryResult({ results: [] }));
  mutateAsync.mockResolvedValue(policy());
  authFetch.mockResolvedValue(
    new Response(new Blob(["zip"]), {
      status: 200,
      headers: {
        "Content-Disposition": 'attachment; filename="gram-observability.zip"',
      },
    }),
  );
});

describe("useSecretGuideOperations", () => {
  it("starts without a selected client", () => {
    const { result: hook } = renderHook(() => useSecretGuideOperations());

    expect(hook.current.clientSelected).toBe(false);
    expect(hook.current.client).toBe("claude");
  });

  it("resumes a matching policy and scopes every generated query to the project", async () => {
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());

    act(() =>
      hook.current.handleSignal({ type: "start", scope: POLICY_SCOPE }, report),
    );

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "success",
        scope: POLICY_SCOPE,
        result: "Secrets policy already live · block on match",
      }),
    );
    expect(mutateAsync).not.toHaveBeenCalled();
    expect(queryHooks.policies).toHaveBeenCalledWith(
      { gramProject: "request-project" },
      undefined,
      { throwOnError: false },
    );
    expect(queryHooks.hooks.mock.calls[0]?.[0]).toMatchObject({
      gramProject: "request-project",
    });
    expect(queryHooks.results.mock.calls[0]?.[0]).toMatchObject({
      gramProject: "request-project",
      category: "secrets",
      limit: 20,
    });
  });

  it("creates the standard blocking policy", async () => {
    queryHooks.policies.mockReturnValue(queryResult({ policies: [] }));
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());

    act(() =>
      hook.current.handleSignal({ type: "start", scope: POLICY_SCOPE }, report),
    );

    await waitFor(() => expect(mutateAsync).toHaveBeenCalledOnce());
    expect(mutateAsync).toHaveBeenCalledWith({
      request: {
        gramProject: "request-project",
        createRiskPolicyRequestBody: {
          action: "block",
          audienceType: "everyone",
          autoName: true,
          enabled: true,
          messageTypes: ["tool_request", "tool_response"],
          policyType: "standard",
          sources: ["gitleaks"],
        },
      },
    });
    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "success",
        scope: POLICY_SCOPE,
        result: "Secrets policy created · block on match",
      }),
    );
    expect(report).toHaveBeenNthCalledWith(1, {
      type: "progress",
      scope: POLICY_SCOPE,
      message: "Detecting and enabling the Secrets category",
      progress: 0.25,
    });
    expect(report).toHaveBeenNthCalledWith(2, {
      type: "progress",
      scope: POLICY_SCOPE,
      message: "Scoping user prompts",
      progress: 0.5,
    });
    expect(report).toHaveBeenNthCalledWith(3, {
      type: "progress",
      scope: POLICY_SCOPE,
      message: "Setting the action to deny",
      progress: 0.75,
    });
  });

  it("reports policy creation errors for retry", async () => {
    queryHooks.policies.mockReturnValue(queryResult({ policies: [] }));
    mutateAsync.mockRejectedValueOnce(new Error("create failed"));
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());

    act(() =>
      hook.current.handleSignal({ type: "start", scope: POLICY_SCOPE }, report),
    );

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "error",
        scope: POLICY_SCOPE,
        message:
          "Could not create the blocking secrets policy. Retry the policy step.",
      }),
    );
  });

  it("treats undefined policy data as unreadable instead of an empty list", async () => {
    queryHooks.policies.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());

    act(() =>
      hook.current.handleSignal({ type: "start", scope: POLICY_SCOPE }, report),
    );

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "error",
        scope: POLICY_SCOPE,
        message:
          "Could not read this project's risk policies. Retry the policy check.",
      }),
    );
    expect(mutateAsync).not.toHaveBeenCalled();
  });

  it("downloads the existing observability ZIP and preserves its response filename", async () => {
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());
    const scope = { ...POLICY_SCOPE, step: 1, runId: 2 };

    act(() => hook.current.setClient("claude"));
    act(() => hook.current.handleSignal({ type: "start", scope }, report));

    await waitFor(() =>
      expect(hook.current.downloadedFilename).toBe("gram-observability.zip"),
    );
    expect(authFetch).toHaveBeenCalledWith(
      "/rpc/plugins.downloadObservabilityPlugin?platform=claude",
      {},
    );
    expect(downloadResponse).toHaveBeenCalledWith(
      expect.any(Response),
      "observability-claude.zip",
    );
    expect(report).toHaveBeenCalledWith({
      type: "success",
      scope,
      result: "Observability plugin downloaded · gram-observability.zip",
    });
    expect(report).toHaveBeenNthCalledWith(1, {
      type: "progress",
      scope,
      message: "Building the Claude Code observability plugin",
      progress: 0.25,
    });
    expect(report).toHaveBeenNthCalledWith(2, {
      type: "progress",
      scope,
      message: "Signing the observability plugin bundle",
      progress: 0.5,
    });
    expect(hook.current.installCommand).toBe(
      "unzip -oq gram-observability.zip -d ~/.claude/plugins/",
    );
  });

  it("waits without reporting or downloading when Step 2 has no client", async () => {
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());
    const scope = { ...POLICY_SCOPE, step: 1, runId: 2 };

    act(() => hook.current.handleSignal({ type: "start", scope }, report));

    await waitFor(() => expect(authFetch).not.toHaveBeenCalled());
    expect(report).not.toHaveBeenCalled();
  });

  it.each([
    {
      client: "cursor" as const,
      filename: "observability-cursor.zip",
      command: "unzip -oq observability-cursor.zip -d ~/.cursor/extensions/",
    },
    {
      client: "codex" as const,
      filename: "observability-codex.zip",
      command: "unzip -oq observability-codex.zip -d ~/.codex/plugins/",
    },
    {
      client: "opencode" as const,
      filename: "observability-opencode.zip",
      command: "unzip -oq observability-opencode.zip -d .opencode/",
    },
  ])(
    "activates the generated $client archive through its existing install contract",
    async ({ client, filename, command }) => {
      authFetch.mockResolvedValueOnce(
        new Response(new Blob(["zip"]), {
          status: 200,
          headers: {
            "Content-Disposition": `attachment; filename="${filename}"`,
          },
        }),
      );
      const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
      const { result: hook } = renderHook(() => useSecretGuideOperations());
      const scope = { ...POLICY_SCOPE, step: 1, runId: 2 };

      act(() => hook.current.setClient(client));
      act(() => hook.current.handleSignal({ type: "start", scope }, report));

      await waitFor(() =>
        expect(hook.current.downloadedFilename).toBe(filename),
      );
      expect(hook.current.installCommand).toBe(command);
      expect(authFetch).toHaveBeenCalledWith(
        `/rpc/plugins.downloadObservabilityPlugin?platform=${client}`,
        {},
      );
    },
  );

  it("captures the hook and risk baselines only after the prompt is copied", async () => {
    const hooks = queryResult({ traces: [trace()] });
    const results = queryResult({ results: [result()] });
    queryHooks.hooks.mockReturnValue(hooks);
    queryHooks.results.mockReturnValue(results);
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());
    const scope = { ...POLICY_SCOPE, step: 3, runId: 2 };

    expect(hooks.refetch).not.toHaveBeenCalled();
    expect(results.refetch).not.toHaveBeenCalled();
    act(() =>
      hook.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 2, runId: 2 } },
        report,
      ),
    );
    expect(hooks.refetch).not.toHaveBeenCalled();
    expect(results.refetch).not.toHaveBeenCalled();
    act(() => hook.current.handleSignal({ type: "checkpoint", scope }, report));

    await waitFor(() => expect(hooks.refetch).toHaveBeenCalledOnce());
    expect(hooks.refetch).toHaveBeenCalledOnce();
    expect(results.refetch).toHaveBeenCalledOnce();
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "event" }),
    );
  });

  it("waits when post-baseline queries have no data yet", async () => {
    const hooks = {
      data: undefined,
      isError: false,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(() =>
        Promise.resolve({ data: { traces: [] }, isError: false }),
      ),
    };
    const results = {
      data: undefined,
      isError: false,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(() =>
        Promise.resolve({ data: { results: [] }, isError: false }),
      ),
    };
    queryHooks.hooks.mockReturnValue(hooks);
    queryHooks.results.mockReturnValue(results);
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());

    act(() =>
      hook.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 3, runId: 2 } },
        report,
      ),
    );
    await waitFor(() => expect(hooks.refetch).toHaveBeenCalledOnce());

    const listenScope = { ...POLICY_SCOPE, step: 4, runId: 3 };
    act(() =>
      hook.current.handleSignal({ type: "start", scope: listenScope }, report),
    );

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "progress",
        scope: listenScope,
        message:
          "Waiting for a new blocked hook and matching secrets risk event.",
      }),
    );
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "error" }),
    );
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "event" }),
    );
  });

  it("keeps waiting when Step 5 starts before a baseline is ready", async () => {
    const hooks = queryResult<{ traces: HookTraceSummary[] }>({ traces: [] });
    const results = queryResult<{ results: RiskResult[] }>({ results: [] });
    hooks.refetch.mockResolvedValueOnce({
      data: { traces: [] },
      isError: true,
    });
    results.refetch.mockResolvedValueOnce({
      data: { results: [] },
      isError: true,
    });
    queryHooks.hooks.mockReturnValue(hooks);
    queryHooks.results.mockReturnValue(results);
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());

    act(() =>
      hook.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 3, runId: 2 } },
        report,
      ),
    );
    await waitFor(() => expect(hook.current.telemetryError).toBe(true));

    const listenScope = { ...POLICY_SCOPE, step: 4, runId: 3 };
    act(() =>
      hook.current.handleSignal({ type: "start", scope: listenScope }, report),
    );

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "progress",
        scope: listenScope,
        message:
          "Waiting for a new blocked hook and matching secrets risk event.",
      }),
    );
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "error" }),
    );
  });

  it("completes only for post-baseline evidence from the selected client and policy", async () => {
    const now = Date.now();
    const hookData = { current: { traces: [trace()] } };
    const riskData = { current: { results: [result()] } };
    const refetchHooks = vi.fn(() =>
      Promise.resolve({ data: hookData.current, isError: false }),
    );
    const refetchResults = vi.fn(() =>
      Promise.resolve({ data: riskData.current, isError: false }),
    );
    queryHooks.hooks.mockImplementation(() => ({
      ...queryResult(hookData.current),
      refetch: refetchHooks,
    }));
    queryHooks.results.mockImplementation(() => ({
      ...queryResult(riskData.current),
      refetch: refetchResults,
    }));
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const view = renderHook(() => useSecretGuideOperations());

    act(() => view.result.current.setClient("claude"));
    act(() =>
      view.result.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 3, runId: 2 } },
        report,
      ),
    );
    await waitFor(() => expect(refetchHooks).toHaveBeenCalledOnce());

    const listenScope = { ...POLICY_SCOPE, step: 4, runId: 3 };
    act(() =>
      view.result.current.handleSignal(
        { type: "start", scope: listenScope },
        report,
      ),
    );
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "event" }),
    );

    hookData.current = {
      traces: [
        trace({
          traceId: "new-blocked-trace",
          startTimeUnixNano: String(BigInt(now + 2_000) * 1_000_000n),
        }),
      ],
    };
    riskData.current = {
      results: [
        result({
          createdAt: new Date(now + 2_000),
          id: "new-result",
          matchRedacted: SYNTHETIC_SECRET_REDACTION,
          ruleId: "secret.github_pat",
        }),
      ],
    };
    view.rerender();

    await waitFor(() =>
      expect(report).toHaveBeenCalledWith({
        type: "event",
        scope: listenScope,
        event: {
          kind: "Denied · risk event",
          tone: "deny",
          title: "request denied by secrets policy",
          rows: [
            { key: "rule", value: "secret.github_pat" },
            { key: "match", value: "synthetic credential" },
          ],
          note: "The prompt was blocked before the model answered.",
        },
      }),
    );
  });

  it("ignores a post-baseline risk result paired with an unrelated blocked trace", async () => {
    const now = Date.now();
    const hookData = { current: { traces: [] as HookTraceSummary[] } };
    const riskData = { current: { results: [] as RiskResult[] } };
    queryHooks.hooks.mockImplementation(() => ({
      ...queryResult(hookData.current),
      refetch: vi.fn(() =>
        Promise.resolve({ data: hookData.current, isError: false }),
      ),
    }));
    queryHooks.results.mockImplementation(() => ({
      ...queryResult(riskData.current),
      refetch: vi.fn(() =>
        Promise.resolve({ data: riskData.current, isError: false }),
      ),
    }));
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const view = renderHook(() => useSecretGuideOperations());

    act(() => view.result.current.setClient("claude"));
    act(() =>
      view.result.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 3, runId: 2 } },
        report,
      ),
    );
    await waitFor(() => expect(queryHooks.hooks).toHaveBeenCalled());
    act(() =>
      view.result.current.handleSignal(
        { type: "start", scope: { ...POLICY_SCOPE, step: 4, runId: 3 } },
        report,
      ),
    );

    hookData.current = {
      traces: [
        trace({
          hookSource: "cursor",
          startTimeUnixNano: String(BigInt(now + 2_000) * 1_000_000n),
          traceId: "wrong-client-trace",
        }),
        trace({
          blockReason:
            'Speakeasy blocked this tool call: matched policy "Another policy" (different request)',
          startTimeUnixNano: String(BigInt(now + 2_000) * 1_000_000n),
          traceId: "wrong-policy-trace",
        }),
      ],
    };
    riskData.current = {
      results: [
        result({
          createdAt: new Date(now + 2_000),
          id: "matching-result",
          matchRedacted: SYNTHETIC_SECRET_REDACTION,
          ruleId: "secret.github_pat",
        }),
      ],
    };
    view.rerender();

    await waitFor(() => expect(queryHooks.results).toHaveBeenCalled());
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "event" }),
    );
  });

  it("ignores delayed historical records that were outside the baseline page", async () => {
    const now = Date.now();
    const hookData = { current: { traces: [] as HookTraceSummary[] } };
    const riskData = { current: { results: [] as RiskResult[] } };
    queryHooks.hooks.mockImplementation(() => ({
      ...queryResult(hookData.current),
      refetch: vi.fn(() =>
        Promise.resolve({ data: hookData.current, isError: false }),
      ),
    }));
    queryHooks.results.mockImplementation(() => ({
      ...queryResult(riskData.current),
      refetch: vi.fn(() =>
        Promise.resolve({ data: riskData.current, isError: false }),
      ),
    }));
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const view = renderHook(() => useSecretGuideOperations());

    act(() =>
      view.result.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 3, runId: 2 } },
        report,
      ),
    );
    await waitFor(() => expect(queryHooks.hooks).toHaveBeenCalled());
    act(() =>
      view.result.current.handleSignal(
        { type: "start", scope: { ...POLICY_SCOPE, step: 4, runId: 3 } },
        report,
      ),
    );

    hookData.current = {
      traces: [
        trace({
          startTimeUnixNano: String(BigInt(now - 60_000) * 1_000_000n),
          traceId: "delayed-old-trace",
        }),
      ],
    };
    riskData.current = {
      results: [
        result({
          createdAt: new Date(now - 60_000),
          id: "delayed-old-result",
          matchRedacted: SYNTHETIC_SECRET_REDACTION,
          ruleId: "secret.github_pat",
        }),
      ],
    };
    view.rerender();

    await waitFor(() => expect(queryHooks.hooks).toHaveBeenCalled());
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "event" }),
    );
  });

  it("ignores otherwise correlated evidence for a different secret prompt", async () => {
    const now = Date.now();
    const hookData = { current: { traces: [] as HookTraceSummary[] } };
    const riskData = { current: { results: [] as RiskResult[] } };
    queryHooks.hooks.mockImplementation(() => ({
      ...queryResult(hookData.current),
      refetch: vi.fn(() =>
        Promise.resolve({ data: hookData.current, isError: false }),
      ),
    }));
    queryHooks.results.mockImplementation(() => ({
      ...queryResult(riskData.current),
      refetch: vi.fn(() =>
        Promise.resolve({ data: riskData.current, isError: false }),
      ),
    }));
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const view = renderHook(() => useSecretGuideOperations());

    act(() =>
      view.result.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 3, runId: 2 } },
        report,
      ),
    );
    await waitFor(() => expect(queryHooks.hooks).toHaveBeenCalled());
    act(() =>
      view.result.current.handleSignal(
        { type: "start", scope: { ...POLICY_SCOPE, step: 4, runId: 3 } },
        report,
      ),
    );

    hookData.current = {
      traces: [
        trace({
          startTimeUnixNano: String(BigInt(now + 2_000) * 1_000_000n),
          traceId: "correlated-trace",
        }),
      ],
    };
    riskData.current = {
      results: [
        result({
          createdAt: new Date(now + 2_000),
          id: "other-secret-result",
          matchRedacted: "<redacted len=37 sha=deadbeef>",
          ruleId: "secret.github_pat",
        }),
      ],
    };
    view.rerender();

    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "event" }),
    );
  });

  it("keeps the dummy-secret prompt copy-gated and retries telemetry without replacing the baseline", async () => {
    const hooks = queryResult({ traces: [] });
    const results = queryResult({ results: [] });
    queryHooks.hooks.mockReturnValue(hooks);
    queryHooks.results.mockReturnValue(results);
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());

    expect(hook.current.prompt).toBe(
      'Run this exact command in your shell:\n\necho "GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab"',
    );
    act(() =>
      hook.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 3, runId: 2 } },
        report,
      ),
    );
    await waitFor(() => expect(hooks.refetch).toHaveBeenCalledOnce());
    act(() =>
      hook.current.handleSignal(
        {
          type: "retry",
          scope: { ...POLICY_SCOPE, step: 4, attempt: 1, runId: 3 },
        },
        report,
      ),
    );

    await waitFor(() => {
      expect(hooks.refetch).toHaveBeenCalledTimes(2);
      expect(results.refetch).toHaveBeenCalledTimes(2);
    });
  });

  it("recaptures a missing baseline through the normal retry signal", async () => {
    const hooks = queryResult<{ traces: HookTraceSummary[] }>({ traces: [] });
    const results = queryResult<{ results: RiskResult[] }>({ results: [] });
    hooks.refetch
      .mockResolvedValueOnce({ data: { traces: [] }, isError: true })
      .mockResolvedValue({ data: { traces: [] }, isError: false });
    results.refetch
      .mockResolvedValueOnce({ data: { results: [] }, isError: true })
      .mockResolvedValue({ data: { results: [] }, isError: false });
    queryHooks.hooks.mockReturnValue(hooks);
    queryHooks.results.mockReturnValue(results);
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());

    act(() =>
      hook.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 3, runId: 2 } },
        report,
      ),
    );
    await waitFor(() => expect(hook.current.telemetryError).toBe(true));

    act(() =>
      hook.current.handleSignal(
        {
          type: "retry",
          scope: { ...POLICY_SCOPE, step: 4, attempt: 1, runId: 3 },
        },
        report,
      ),
    );

    await waitFor(() => expect(hooks.refetch).toHaveBeenCalledTimes(2));
    expect(hooks.refetch).toHaveBeenCalledTimes(2);
    expect(results.refetch).toHaveBeenCalledTimes(2);
  });
});
