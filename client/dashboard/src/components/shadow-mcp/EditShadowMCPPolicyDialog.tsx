import { audienceGroups } from "@/components/mcp-approvals/audienceGroups";
import { invalidateShadowMCPPolicyInventory } from "@/components/shadow-mcp/useShadowMCPPolicyInventory";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Skeleton } from "@/components/ui/Skeleton";
import { Spinner } from "@/components/ui/Spinner";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import { useProject } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import type { ListShadowMCPInventoryResult } from "@gram/client/models/components/listshadowmcpinventoryresult.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useCreateMcpApprovalRequestMutation } from "@gram/client/react-query/createMcpApprovalRequest.js";
import { invalidateAllListMcpApprovalRequests } from "@gram/client/react-query/listMcpApprovalRequests.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { usePromoteMcpApprovalRequestMutation } from "@gram/client/react-query/promoteMcpApprovalRequest.js";
import { useRecordMcpApprovalDecisionMutation } from "@gram/client/react-query/recordMcpApprovalDecision.js";
import { useRiskCreatePolicyMutation } from "@gram/client/react-query/riskCreatePolicy.js";
import { invalidateAllRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRiskPoliciesDeleteMutation } from "@gram/client/react-query/riskPoliciesDelete.js";
import { useRiskPoliciesUpdateMutation } from "@gram/client/react-query/riskPoliciesUpdate.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import {
  buildShadowMCPInventoryQuery,
  invalidateAllShadowMCPInventory,
} from "@gram/client/react-query/shadowMCPInventory.js";
import { invalidateAllShadowMCPInventoryServer } from "@gram/client/react-query/shadowMCPInventoryServer.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY,
  type ShadowMCPMode,
  type ShadowMCPModeState,
} from "./shadowMCPMode";
import {
  keepAsTodayDecision,
  serversChangedByPolicy,
  shadowMCPServerLabel,
  type StandingDecision,
} from "./shadowMCPPolicyChange";

/** The wizard reads the whole inventory, a page at a time, up to this many. */
const POLICY_CHANGE_PAGE_LIMIT = 200;
const POLICY_CHANGE_MAX_PAGES = 10;

const KEEP_RATIONALE: Record<ShadowMCPMode, string> = {
  blocked: "Approved so it keeps working when servers are blocked by default.",
  allowed: "Blocked so it stays blocked when servers are allowed by default.",
};

const MODE_COPY: Record<
  ShadowMCPMode,
  { title: string; detail: string; confirm: string; done: string }
> = {
  allowed: {
    title: "Allow by default",
    detail:
      "Every MCP server connects unless you block it. Servers you blocked stay blocked.",
    confirm: "Allow by default",
    done: "MCP servers are now allowed by default.",
  },
  blocked: {
    title: "Block by default",
    detail:
      "Every MCP server is blocked for everyone until you approve it; narrow who it applies to under Advanced. Servers you approved keep working.",
    confirm: "Block by default",
    done: "MCP servers are now blocked by default.",
  },
};

type Step = "choose" | "review";

/**
 * Reads every server the project knows about for the review step. The
 * table pages through the inventory as the admin scrolls; the wizard needs
 * all of it at once to say which servers a change touches.
 */
function useWholeInventory(projectId: string, enabled: boolean) {
  const client = useGramContext();
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: ["shadow-mcp-policy-change", projectId],
    enabled,
    staleTime: 0,
    gcTime: 0,
    queryFn: async () => {
      const servers: ShadowMCPInventoryServer[] = [];
      let cursor: string | undefined = undefined;
      for (let page = 0; page < POLICY_CHANGE_MAX_PAGES; page++) {
        const result: ListShadowMCPInventoryResult =
          await queryClient.fetchQuery({
            ...buildShadowMCPInventoryQuery(client, {
              projectId,
              limit: POLICY_CHANGE_PAGE_LIMIT,
              cursor,
            }),
            staleTime: 0,
          });
        servers.push(...result.servers);
        if (!result.nextCursor) return { servers, truncated: false };
        cursor = result.nextCursor;
      }
      return { servers, truncated: true };
    },
  });
}

/**
 * Changes what happens to an MCP server nobody decided on. Allowed by
 * default is built in; blocked by default is a policy this dialog creates,
 * narrows and removes. Before a flip it lists every server whose behaviour
 * changes, most blocked and most used first, and under Advanced records a
 * standing decision on the ones the admin wants to keep as they are today.
 */
export function EditShadowMCPPolicyDialog({
  open,
  onOpenChange,
  state,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  state: ShadowMCPModeState;
}): JSX.Element {
  const project = useProject();
  const queryClient = useQueryClient();
  const membersQuery = useMembers(undefined, undefined, {
    throwOnError: false,
  });
  const rolesQuery = useRoles(undefined, undefined, { throwOnError: false });
  const createPolicy = useRiskCreatePolicyMutation();
  const updatePolicy = useRiskPoliciesUpdateMutation();
  const removePolicy = useRiskPoliciesDeleteMutation();
  const createRequest = useCreateMcpApprovalRequestMutation();
  const promoteRequest = usePromoteMcpApprovalRequestMutation();
  const decide = useRecordMcpApprovalDecisionMutation();

  const currentPrincipals =
    state.policy?.audienceType === "targeted"
      ? state.policy.audiencePrincipalUrns
      : [];
  const [step, setStep] = useState<Step>("choose");
  const [target, setTarget] = useState<ShadowMCPMode>(state.mode);
  const [principals, setPrincipals] = useState<string[]>(currentPrincipals);
  const [keep, setKeep] = useState<Set<string>>(new Set());
  const [rationale, setRationale] = useState(KEEP_RATIONALE[state.mode]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setStep("choose");
    setTarget(state.mode);
    setPrincipals(currentPrincipals);
    setKeep(new Set());
    setError(null);
    setSaving(false);
    // Reset only when the dialog opens: the policy read refreshes while it
    // is open, and that must not throw away the admin's choices.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const flips = target !== state.mode;
  const inventory = useWholeInventory(project.id, open && flips);
  const changed = useMemo(
    () =>
      serversChangedByPolicy(inventory.data?.servers ?? [], state.mode, target),
    [inventory.data?.servers, state.mode, target],
  );
  const groups = audienceGroups(
    membersQuery.data?.members ?? [],
    rolesQuery.data?.roles ?? [],
  );
  const audienceChanged =
    target === "blocked" && !flips && !sameSet(principals, currentPrincipals);
  const keepDecision = keepAsTodayDecision(target);
  const keptServers = changed.filter((server) =>
    keep.has(server.canonicalServerUrl),
  );

  const chooseTarget = (next: ShadowMCPMode) => {
    setTarget(next);
    setKeep(new Set());
    setRationale(KEEP_RATIONALE[next]);
    setError(null);
  };

  const recordKeepDecision = async (
    server: ShadowMCPInventoryServer,
    decision: StandingDecision,
    note: string,
  ) => {
    let requestId = server.approvalRequest?.id;
    // A pending legacy ask is promoted so it resolves with this decision,
    // the same road the Decide access sheet takes.
    if (server.latestRequest?.id) {
      const summary = await promoteRequest.mutateAsync({
        request: {
          gramProject: project.slug,
          promoteRequestBody: {
            riskPolicyBypassRequestId: server.latestRequest.id,
          },
        },
      });
      requestId = summary.id;
    }
    if (!requestId) {
      const summary = await createRequest.mutateAsync({
        request: {
          gramProject: project.slug,
          createRequestRequestBody: {
            targetKind: "server_url",
            target: server.canonicalServerUrl,
            note,
          },
        },
      });
      requestId = summary.id;
    }
    await decide.mutateAsync({
      request: {
        gramProject: project.slug,
        recordDecisionRequestBody: { id: requestId, decision, rationale: note },
      },
    });
  };

  const audience: {
    audienceType: "targeted" | "everyone";
    audiencePrincipalUrns: string[];
  } = {
    audienceType: principals.length > 0 ? "targeted" : "everyone",
    audiencePrincipalUrns: [...principals],
  };

  const applyPolicy = async () => {
    if (target === "blocked" && flips) {
      await createPolicy.mutateAsync({
        request: {
          createRiskPolicyRequestBody: {
            ...SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY,
            sources: [...SHADOW_MCP_BLOCKED_BY_DEFAULT_POLICY.sources],
            ...audience,
          },
        },
      });
      return;
    }
    if (target === "blocked" && state.policy) {
      await updatePolicy.mutateAsync({
        request: {
          updateRiskPolicyRequestBody: {
            id: state.policy.id,
            name: state.policy.name,
            sources: state.policy.sources,
            action: "block",
            ...audience,
          },
        },
      });
      return;
    }
    if (target === "allowed" && state.policy) {
      await removePolicy.mutateAsync({ request: { id: state.policy.id } });
    }
  };

  const refresh = () =>
    Promise.all([
      invalidateAllRiskListPolicies(queryClient),
      invalidateAllShadowMCPInventory(queryClient),
      invalidateAllShadowMCPInventoryServer(queryClient),
      invalidateAllListMcpApprovalRequests(queryClient),
      invalidateShadowMCPPolicyInventory(queryClient, project.id),
      queryClient.invalidateQueries({
        queryKey: ["shadow-mcp-policy-change", project.id],
      }),
    ]);

  const save = async () => {
    setError(null);
    setSaving(true);
    const note = rationale.trim() || KEEP_RATIONALE[target];
    // Decisions go first: an approval recorded before the block lands means
    // a kept server never sees a blocked call, and a denial recorded before
    // the policy goes away keeps its server blocked throughout.
    for (const server of keptServers) {
      try {
        await recordKeepDecision(server, keepDecision, note);
      } catch {
        await refresh();
        setSaving(false);
        setError(
          `Recording a decision on ${shadowMCPServerLabel(server)} failed, so the policy was not changed. Servers decided before it keep their decision. Try again.`,
        );
        return;
      }
    }
    try {
      await applyPolicy();
    } catch (err) {
      await refresh();
      setSaving(false);
      setError(policyErrorMessage(err, keptServers.length));
      return;
    }
    await refresh();
    setSaving(false);
    toast.success(
      audienceChanged
        ? "Saved who is blocked by default."
        : MODE_COPY[target].done,
    );
    onOpenChange(false);
  };

  const canContinue = flips || audienceChanged;
  const confirmLabel = keptServers.length
    ? `${MODE_COPY[target].confirm} and ${keepDecision === "approved" ? "approve" : "block"} ${keptServers.length}`
    : MODE_COPY[target].confirm;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && saving) return;
        onOpenChange(next);
      }}
    >
      <Dialog.Content closeable={!saving} className="sm:max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>
            {step === "choose"
              ? "Edit Shadow MCP policy"
              : "Review what changes"}
          </Dialog.Title>
          <Dialog.Description>
            {step === "choose"
              ? "Choose what happens to an MCP server nobody has approved or blocked yet."
              : reviewDescription(
                  target,
                  principals.length > 0,
                  changed.length,
                )}
          </Dialog.Description>
        </Dialog.Header>

        {step === "choose" ? (
          <ChooseStep
            current={state.mode}
            target={target}
            onTarget={chooseTarget}
            groups={groups}
            principals={principals}
            onPrincipals={setPrincipals}
          />
        ) : (
          <ReviewStep
            target={target}
            loading={inventory.isLoading}
            failed={inventory.isError}
            truncated={inventory.data?.truncated ?? false}
            onRetry={() => void inventory.refetch()}
            servers={changed}
            keep={keep}
            onKeep={setKeep}
            keepDecision={keepDecision}
            rationale={rationale}
            onRationale={setRationale}
            disabled={saving}
          />
        )}

        {error ? (
          <Alert variant="error" title={null}>
            {error}
          </Alert>
        ) : null}

        <Dialog.Footer>
          {step === "review" ? (
            <Button
              variant="secondary"
              disabled={saving}
              onClick={() => {
                setError(null);
                setStep("choose");
              }}
            >
              <Button.Text>Back</Button.Text>
            </Button>
          ) : (
            <Button
              variant="secondary"
              disabled={saving}
              onClick={() => onOpenChange(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          )}
          {step === "choose" && flips ? (
            <Button disabled={!canContinue} onClick={() => setStep("review")}>
              <Button.Text>Review changes</Button.Text>
            </Button>
          ) : (
            <Button
              variant={
                target === "blocked" && flips
                  ? "destructive-primary"
                  : "primary"
              }
              disabled={
                !canContinue ||
                saving ||
                (step === "review" && !inventory.isSuccess)
              }
              onClick={() => void save()}
            >
              <Button.LeftIcon>
                {saving ? <Spinner className="mr-0" /> : null}
              </Button.LeftIcon>
              <Button.Text>
                {saving ? "Saving…" : audienceChanged ? "Save" : confirmLabel}
              </Button.Text>
            </Button>
          )}
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function reviewDescription(
  target: ShadowMCPMode,
  narrowed: boolean,
  count: number,
): string {
  if (count === 0) {
    return "No servers change under this policy.";
  }
  const servers = count === 1 ? "1 server has" : `${count} servers have`;
  if (target === "blocked") {
    const who = narrowed
      ? "for the people and roles you chose"
      : "for everyone";
    return `${servers} no decision yet and will be blocked ${who}. Servers people use or that were blocked before are listed first.`;
  }
  return `${servers} no decision yet and will connect again. Servers people use or that were blocked before are listed first.`;
}

/**
 * What to tell the admin when the policy write fails. A refusal from the
 * server says exactly what stands in the way, such as a denial narrowed to
 * some people under block by default for everyone, so it is shown as is.
 */
function policyErrorMessage(err: unknown, kept: number): string {
  const decided =
    kept > 0
      ? ` The ${kept === 1 ? "decision" : `${kept} decisions`} you picked ${kept === 1 ? "was" : "were"} recorded.`
      : "";
  if (err instanceof ServiceError && err.message) {
    return `The policy was not changed. ${err.message}${decided}`;
  }
  return `The policy could not be changed. Try again.${decided}`;
}

function sameSet(a: readonly string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((value) => set.has(value));
}

function ChooseStep({
  current,
  target,
  onTarget,
  groups,
  principals,
  onPrincipals,
}: {
  current: ShadowMCPMode;
  target: ShadowMCPMode;
  onTarget: (next: ShadowMCPMode) => void;
  groups: ReturnType<typeof audienceGroups>;
  principals: string[];
  onPrincipals: (next: string[]) => void;
}) {
  const modes: ShadowMCPMode[] = ["allowed", "blocked"];
  return (
    <div className="space-y-4">
      <RadioCardGroup
        value={target}
        onValueChange={(value) => onTarget(value as ShadowMCPMode)}
        aria-label="What happens to a server nobody decided on"
      >
        {modes.map((mode) => (
          <RadioCard
            key={mode}
            value={mode}
            title={MODE_COPY[mode].title}
            trailing={
              mode === current ? (
                <Badge variant="neutral">
                  <Badge.Text>Current</Badge.Text>
                </Badge>
              ) : undefined
            }
          >
            {MODE_COPY[mode].detail}
          </RadioCard>
        ))}
      </RadioCardGroup>
      {target === "blocked" ? (
        <Collapsible defaultOpen={principals.length > 0}>
          <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors [&[data-state=open]>svg]:rotate-90">
            <Icon name="chevron-right" className="size-3.5" aria-hidden />
            Advanced
          </CollapsibleTrigger>
          <CollapsibleContent className="mt-3 space-y-1.5">
            <Text variant="small" className="font-medium">
              Who it applies to
            </Text>
            <MultiSelect
              options={groups}
              defaultValue={principals}
              onValueChange={onPrincipals}
              placeholder="Everyone in the project"
            />
            <Text muted small>
              Pick people or roles to block by default only for them. Leave
              empty for everyone.
            </Text>
          </CollapsibleContent>
        </Collapsible>
      ) : null}
    </div>
  );
}

function ReviewStep({
  target,
  loading,
  failed,
  truncated,
  onRetry,
  servers,
  keep,
  onKeep,
  keepDecision,
  rationale,
  onRationale,
  disabled,
}: {
  target: ShadowMCPMode;
  loading: boolean;
  failed: boolean;
  truncated: boolean;
  onRetry: () => void;
  servers: ShadowMCPInventoryServer[];
  keep: Set<string>;
  onKeep: (next: Set<string>) => void;
  keepDecision: StandingDecision;
  rationale: string;
  onRationale: (next: string) => void;
  disabled: boolean;
}) {
  const [advanced, setAdvanced] = useState(false);
  if (loading) {
    return (
      <div aria-label="Loading servers" role="status" className="space-y-2">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }
  if (failed) {
    return (
      <Alert variant="warning" title="Servers could not be loaded">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <span>
            The list of servers that change could not be loaded. Retry to review
            it before changing the policy.
          </span>
          <Button variant="secondary" size="sm" onClick={onRetry}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </div>
      </Alert>
    );
  }
  if (servers.length === 0) return null;

  const allKept = servers.every((server) =>
    keep.has(server.canonicalServerUrl),
  );
  const toggle = (url: string, checked: boolean) => {
    const next = new Set(keep);
    if (checked) next.add(url);
    if (!checked) next.delete(url);
    onKeep(next);
  };
  const before = target === "blocked" ? "Allowed" : "Blocked";
  const after = target === "blocked" ? "Blocked" : "Allowed";
  const keepColumn: Column<ShadowMCPInventoryServer> = {
    key: "keep",
    header: (
      <Checkbox
        aria-label="Keep every server as it is today"
        checked={allKept}
        disabled={disabled}
        onCheckedChange={(checked) =>
          onKeep(
            checked === true
              ? new Set(servers.map((server) => server.canonicalServerUrl))
              : new Set(),
          )
        }
      />
    ),
    width: "40px",
    render: (server) => (
      <Checkbox
        aria-label={`Keep ${shadowMCPServerLabel(server)} as it is today`}
        checked={keep.has(server.canonicalServerUrl)}
        disabled={disabled}
        onCheckedChange={(checked) =>
          toggle(server.canonicalServerUrl, checked === true)
        }
      />
    ),
  };
  const columns: Column<ShadowMCPInventoryServer>[] = [
    ...(advanced ? [keepColumn] : []),
    {
      key: "server",
      header: "Server",
      width: "2fr",
      render: (server) => (
        <div className="min-w-0">
          <Text variant="small" className="truncate font-medium">
            {shadowMCPServerLabel(server)}
          </Text>
          {server.urlHost && server.urlHost !== shadowMCPServerLabel(server) ? (
            <Text muted small className="truncate text-xs">
              {server.urlHost}
            </Text>
          ) : null}
        </div>
      ),
    },
    {
      key: "calls",
      header: "Calls",
      width: "0.6fr",
      render: (server) => (
        <Text variant="small" className="tabular-nums">
          {server.observedUseCount.toLocaleString()}
        </Text>
      ),
    },
    {
      key: "blocked",
      header: "Blocked",
      width: "0.6fr",
      render: (server) => (
        <Text
          variant="small"
          className={cn(
            "tabular-nums",
            server.blockedCallCount > 0 && "text-default-destructive",
          )}
          muted={server.blockedCallCount === 0}
        >
          {server.blockedCallCount.toLocaleString()}
        </Text>
      ),
    },
    {
      key: "change",
      header: "Change",
      width: "1fr",
      render: (server) =>
        keep.has(server.canonicalServerUrl) ? (
          <Text muted small>
            Stays {before.toLowerCase()}
          </Text>
        ) : (
          <Text small>
            {before} → {after}
          </Text>
        ),
    },
  ];

  return (
    <div className="space-y-3">
      <Table
        columns={columns}
        data={servers}
        rowKey={(server) => server.canonicalServerUrl}
        cellPadding="condensed"
        className="max-h-80 overflow-y-auto"
      />
      {truncated ? (
        <Text muted small>
          Showing the first {servers.length.toLocaleString()} servers.
        </Text>
      ) : null}
      <Collapsible open={advanced} onOpenChange={setAdvanced}>
        <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors [&[data-state=open]>svg]:rotate-90">
          <Icon name="chevron-right" className="size-3.5" aria-hidden />
          Advanced
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-3 space-y-2">
          <Text muted small>
            {keepDecision === "approved"
              ? "Tick servers to approve them for everyone first, so they keep working."
              : "Tick servers to block them for everyone first, so they stay blocked."}{" "}
            Each one gets a standing decision with this rationale.
          </Text>
          <TextArea
            aria-label="Rationale"
            value={rationale}
            onChange={onRationale}
            rows={2}
            disabled={disabled}
            className="resize-none text-sm"
          />
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}
