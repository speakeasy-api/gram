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
    expect(hook.current.installCommand).toContain(
      '"$HOME/Downloads"/gram-observability.zip',
    );
    expect(hook.current.installCommand).toContain("claude --plugin-dir");
    expect(hook.current.installInstructions).toMatch(
      /launches Claude Code with the extracted plugin active/i,
    );
  });

  it.each([
    {
      client: "cursor" as const,
      filename: "observability-cursor.zip",
      commandPart:
        'unzip -oq "$HOME/Downloads"/observability-cursor.zip -d "$HOME/gram-observability-cursor"',
      instructions: /Settings.*Plugins.*Import/i,
    },
    {
      client: "codex" as const,
      filename: "observability-codex.zip",
      commandPart: 'bash "$HOME/gram-observability-codex/install.sh"',
      instructions: /registers the marketplace.*approves the hooks/i,
    },
  ])(
    "activates the generated $client archive through its existing install contract",
    async ({ client, filename, commandPart, instructions }) => {
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
      expect(hook.current.installCommand).toContain(
        `"$HOME/Downloads"/${filename}`,
      );
      expect(hook.current.installCommand).toContain(commandPart);
      expect(hook.current.installInstructions).toMatch(instructions);
    },
  );

  it("captures the hook and risk baselines only at the install/restart checkpoint", async () => {
    const hooks = queryResult({ traces: [trace()] });
    const results = queryResult({ results: [result()] });
    queryHooks.hooks.mockReturnValue(hooks);
    queryHooks.results.mockReturnValue(results);
    const report = vi.fn<(report: ProjectGuideOperationReport) => void>();
    const { result: hook } = renderHook(() => useSecretGuideOperations());
    const scope = { ...POLICY_SCOPE, step: 2, runId: 2 };

    expect(hooks.refetch).not.toHaveBeenCalled();
    expect(results.refetch).not.toHaveBeenCalled();
    act(() => hook.current.handleSignal({ type: "checkpoint", scope }, report));

    await waitFor(() => expect(hook.current.telemetryBaselineReady).toBe(true));
    expect(hooks.refetch).toHaveBeenCalledOnce();
    expect(results.refetch).toHaveBeenCalledOnce();
    expect(report).not.toHaveBeenCalledWith(
      expect.objectContaining({ type: "event" }),
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

    act(() =>
      view.result.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 2, runId: 2 } },
        report,
      ),
    );
    await waitFor(() =>
      expect(view.result.current.telemetryBaselineReady).toBe(true),
    );

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
          matchRedacted: `ghp_${"*".repeat(31)}ab`,
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

    act(() =>
      view.result.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 2, runId: 2 } },
        report,
      ),
    );
    await waitFor(() =>
      expect(view.result.current.telemetryBaselineReady).toBe(true),
    );
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
          matchRedacted: `ghp_${"*".repeat(31)}ab`,
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
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 2, runId: 2 } },
        report,
      ),
    );
    await waitFor(() =>
      expect(view.result.current.telemetryBaselineReady).toBe(true),
    );
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
          matchRedacted: `ghp_${"*".repeat(31)}ab`,
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
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 2, runId: 2 } },
        report,
      ),
    );
    await waitFor(() =>
      expect(view.result.current.telemetryBaselineReady).toBe(true),
    );
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
          match: "ghp_000000000000000000000000000000000",
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

    expect(hook.current.prompt).toMatch(/dummy secret/i);
    expect(hook.current.prompt).toMatch(
      /use your local shell tool exactly once/i,
    );
    expect(hook.current.prompt).toMatch(/do not use the network/i);
    expect(hook.current.prompt).toContain(
      "printf '%s\\n' 'GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab' >/dev/null",
    );
    expect(hook.current.promptCopied).toBe(false);
    act(() => hook.current.markPromptCopied());
    expect(hook.current.promptCopied).toBe(true);

    act(() =>
      hook.current.handleSignal(
        { type: "checkpoint", scope: { ...POLICY_SCOPE, step: 2, runId: 2 } },
        report,
      ),
    );
    await waitFor(() => expect(hook.current.telemetryBaselineReady).toBe(true));
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
    expect(hook.current.telemetryBaselineReady).toBe(true);
  });
});
