import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { dateTimeFormatters } from "@/lib/dates";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "@/pages/identities/IdentityPanel";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type {
  WorkloadDeviceJoinTokenSummary,
  WorkloadDeviceJoinTokenSummaryStatus,
} from "@gram/client/models/components/workloaddevicejointokensummary.js";
import { useAgentsCreatePolicyGrantMutation } from "@gram/client/react-query/agentsCreatePolicyGrant.js";
import {
  invalidateAgentsListPolicyGrants,
  useAgentsListPolicyGrants,
} from "@gram/client/react-query/agentsListPolicyGrants.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRevokeWorkloadDeviceJoinTokenMutation } from "@gram/client/react-query/revokeWorkloadDeviceJoinToken.js";
import { useWithdrawWorkloadSubjectMutation } from "@gram/client/react-query/withdrawWorkloadSubject.js";
import {
  invalidateAllWorkloadDeviceJoinTokens,
  useWorkloadDeviceJoinTokensInfinite,
} from "@gram/client/react-query/workloadDeviceJoinTokens.js";
import { invalidateAllWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useState, type JSX } from "react";
import { toast } from "sonner";
import { useAgentAdmissions } from "../agentAdmissions";
import { invalidateAgentPolicy } from "../agent-policy-grants";
import {
  DEFAULT_TOKEN_LIFETIME_SECONDS,
  acceptsHooks,
  allowsDeviceSync,
  deviceEnrollmentPolicyGrants,
  hooksIngestGrant,
  isDeviceAdmission,
  relativeTime,
  type WatchedToken,
} from "./device-enrollment";
import { DeviceEnrollmentFields } from "./DeviceEnrollmentFields";
import { useHooksProjects } from "./useHooksProjects";
import { DeviceSetupPanel } from "./DeviceSetupPanel";

/**
 * The headless boxes enrolled as this agent, and the bootstrap tokens that
 * enroll them. Shown beside the agent's API keys: both are ways a runtime
 * becomes this agent.
 */
export function AgentBootstrapTokens({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element | null {
  const { hasAnyScope } = useRBAC();
  // Tokens and devices are both workload identities, read under one scope.
  if (!hasAnyScope(["workload:read", "workload:write"])) {
    return (
      <Text muted small>
        You need permission to view workload identities to see this
        agent&rsquo;s bootstrap tokens and enrolled devices.
      </Text>
    );
  }
  return <AgentBootstrapTokensContent agent={agent} />;
}

const STATUS: Record<
  WorkloadDeviceJoinTokenSummaryStatus,
  {
    label: string;
    variant: "neutral" | "information" | "success" | "destructive";
  }
> = {
  active: { label: "Active", variant: "information" },
  redeemed: { label: "Redeemed", variant: "success" },
  expired: { label: "Expired", variant: "neutral" },
  revoked: { label: "Revoked", variant: "destructive" },
};

function When({ date }: { date: Date }): JSX.Element {
  return (
    <time
      className="tabular-nums"
      title={dateTimeFormatters.full.format(date)}
      dateTime={date.toISOString()}
    >
      {relativeTime(date)}
    </time>
  );
}

function AgentBootstrapTokensContent({
  agent,
}: {
  agent: ManagedAgent;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { user } = useSession();
  const members = useMembers(undefined, undefined, { throwOnError: false });
  const tokens = useWorkloadDeviceJoinTokensInfinite(
    { agentId: agent.id },
    undefined,
    { throwOnError: false },
  );
  const admissions = useAgentAdmissions(agent.id);
  const devices = admissions.admissions.filter(isDeviceAdmission);
  const [issuing, setIssuing] = useState(false);
  const [revoking, setRevoking] =
    useState<WorkloadDeviceJoinTokenSummary | null>(null);
  const [withdrawing, setWithdrawing] = useState<WorkloadAdmission | null>(
    null,
  );

  // Both confirm dialogs show their own failure, so no global toast.
  const revoke = useRevokeWorkloadDeviceJoinTokenMutation({
    onError: () => undefined,
    onSuccess: async () => {
      await invalidateAllWorkloadDeviceJoinTokens(queryClient);
      setRevoking(null);
      toast.success("Bootstrap token revoked");
    },
  });
  const withdraw = useWithdrawWorkloadSubjectMutation({
    onError: () => undefined,
    onSuccess: async () => {
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setWithdrawing(null);
      toast.success("Device removed");
    },
  });

  const rows = (tokens.data?.pages ?? []).flatMap((page) => page.result.items);
  const creatorName = (userId: string): string => {
    if (userId === user.id) return "You";
    const member = members.data?.members.find(
      (candidate) => candidate.id === userId,
    );
    return member ? member.name || member.email : "Former member";
  };
  const deviceName = (subject: string | undefined): string => {
    if (!subject) return "—";
    return (
      admissions.admissions.find((admission) => admission.subject === subject)
        ?.name || "Unnamed device"
    );
  };

  const columns: Column<WorkloadDeviceJoinTokenSummary>[] = [
    {
      key: "status",
      header: "Status",
      width: "120px",
      render: (token) => (
        <Badge variant={STATUS[token.status].variant} size="sm">
          {STATUS[token.status].label}
        </Badge>
      ),
    },
    {
      key: "createdByUserId",
      header: "Created by",
      render: (token) => (
        <span className="truncate">{creatorName(token.createdByUserId)}</span>
      ),
    },
    {
      key: "createdAt",
      header: "Created",
      width: "110px",
      render: (token) => <When date={token.createdAt} />,
    },
    {
      key: "expiresAt",
      header: "Expires",
      width: "110px",
      // A redeemed or revoked token's expiry no longer matters.
      render: (token) =>
        token.status === "active" || token.status === "expired" ? (
          <When date={token.expiresAt} />
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      key: "consumedBySubject",
      header: "Enrolled",
      render: (token) =>
        token.consumedBySubject ? (
          <span className="truncate" title={token.consumedBySubject}>
            {deviceName(token.consumedBySubject)}
          </span>
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      key: "id",
      header: "",
      width: "110px",
      render: (token) =>
        token.status === "active" ? (
          <RequireScope scope="workload:write" level="component">
            <Button
              size="sm"
              variant="destructive-secondary"
              aria-label="Revoke bootstrap token"
              onClick={() => {
                revoke.reset();
                setRevoking(token);
              }}
            >
              Revoke
            </Button>
          </RequireScope>
        ) : null,
    },
  ];

  const issueButton = (
    <RequireScope scope="workload:write" level="component">
      <Button
        size="sm"
        variant={rows.length > 0 ? "secondary" : "primary"}
        onClick={() => setIssuing(true)}
      >
        Issue bootstrap token
      </Button>
    </RequireScope>
  );

  return (
    <>
      <IdentityPanel
        title="Bootstrap tokens"
        action={issueButton}
        loading={tokens.isPending}
        loadingRows={2}
        error={tokens.isError}
        onRetry={() => void tokens.refetch()}
        footer={
          rows.length === 0
            ? undefined
            : "Each token enrolls one headless box, once."
        }
      >
        {rows.length === 0 ? (
          <IdentityPanelEmpty>
            No bootstrap tokens yet. Issue one to enroll a headless box, like a
            VPS or sandbox, as this agent.
          </IdentityPanelEmpty>
        ) : (
          <Table
            columns={columns}
            data={rows}
            rowKey={(token) => token.id}
            // The panel already draws the frame.
            className="border-0"
            hasMore={tokens.hasNextPage}
            onLoadMore={async () => {
              await tokens.fetchNextPage();
            }}
          />
        )}
      </IdentityPanel>

      <IdentityPanel
        title="Enrolled devices"
        loading={admissions.isPending}
        loadingRows={1}
        error={admissions.isError}
        onRetry={admissions.refetch}
      >
        {devices.length === 0 ? (
          <IdentityPanelEmpty>
            No device has enrolled as this agent. A box appears here once it
            redeems a bootstrap token.
          </IdentityPanelEmpty>
        ) : (
          devices.map((device) => (
            <IdentityPanelRow
              key={device.id}
              title={
                <span title={device.subject}>
                  {device.name || "Unnamed device"}
                </span>
              }
              detail={`Enrolled ${relativeTime(device.createdAt)}`}
              trailing={
                <RequireScope scope="workload:write" level="component">
                  <Button
                    variant="destructive-secondary"
                    size="sm"
                    aria-label={`Remove ${device.name || "device"}`}
                    onClick={() => {
                      withdraw.reset();
                      setWithdrawing(device);
                    }}
                  >
                    <Button.Text>Remove</Button.Text>
                  </Button>
                </RequireScope>
              }
            />
          ))
        )}
      </IdentityPanel>

      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => {
          if (!open && !revoke.isPending) setRevoking(null);
        }}
        title="Revoke this bootstrap token?"
        description="No box can enroll with it any more. Boxes it already enrolled keep working."
        confirmLabel="Revoke token"
        pendingLabel="Revoking…"
        isPending={revoke.isPending}
        error={
          revoke.isError
            ? "Could not revoke this bootstrap token. Try again."
            : undefined
        }
        onConfirm={() => {
          if (revoking) revoke.mutate({ request: { id: revoking.id } });
        }}
      />

      <ConfirmDialog
        open={withdrawing !== null}
        onOpenChange={(open) => {
          if (!open && !withdraw.isPending) setWithdrawing(null);
        }}
        title={`Remove ${withdrawing?.name || "this device"}?`}
        description={`It loses access as ${agent.name} on its next request. To bring it back, enroll it again with a new bootstrap token.`}
        confirmLabel="Remove device"
        pendingLabel="Removing…"
        isPending={withdraw.isPending}
        error={
          withdraw.isError
            ? "Could not remove this device. Try again."
            : undefined
        }
        onConfirm={() => {
          if (withdrawing) withdraw.mutate({ request: { id: withdrawing.id } });
        }}
      />

      <IssueBootstrapTokenSheet
        agent={agent}
        open={issuing}
        onOpenChange={setIssuing}
      />
    </>
  );
}

/**
 * Issuing a bootstrap token for an agent that already exists: the same
 * choices and the same setup as the agent wizard, in a side panel.
 */
export function IssueBootstrapTokenSheet({
  agent,
  open,
  onOpenChange,
}: {
  agent: ManagedAgent;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const project = useProject();
  const { user } = useSession();
  const projects = useHooksProjects();
  const [projectId, setProjectId] = useState<string | null>(null);
  const hooksProjectId = projectId ?? agent.projectId ?? project.id ?? "";
  const hooksProject = projects.find(
    (candidate) => candidate.id === hooksProjectId,
  );
  const [lifetime, setLifetime] = useState(DEFAULT_TOKEN_LIFETIME_SECONDS);
  // Choices are settled once setup starts: the token is created for them.
  const [started, setStarted] = useState(false);

  const grants = useAgentsListPolicyGrants({ agentId: agent.id }, undefined, {
    enabled: open,
    throwOnError: false,
  });
  const policy = grants.data ?? [];
  const canSync = allowsDeviceSync(policy, organization.id);
  const hooksAccepted =
    hooksProject !== undefined &&
    acceptsHooks(policy, organization.id, hooksProject.id);

  const createGrant = useAgentsCreatePolicyGrantMutation({
    onError: () => undefined,
  });
  const [granting, setGranting] = useState(false);
  const [grantError, setGrantError] = useState<string | null>(null);
  const grant = async (
    forms: ReturnType<typeof deviceEnrollmentPolicyGrants>,
  ) => {
    setGranting(true);
    setGrantError(null);
    try {
      for (const form of forms) {
        await createGrant.mutateAsync({
          request: {
            createAgentPolicyGrantForm: {
              agentId: agent.id,
              effect: "allow",
              scope: form.scope,
              selector: form.selector,
            },
          },
        });
      }
      await Promise.all([
        invalidateAgentsListPolicyGrants(queryClient, [{ agentId: agent.id }]),
        invalidateAgentPolicy(queryClient, organization.id, user.id, agent.id),
      ]);
    } catch (failure) {
      setGrantError(
        failure instanceof Error && failure.message
          ? failure.message
          : "Could not change this agent's permissions. Try again.",
      );
    } finally {
      setGranting(false);
    }
  };

  const onTokenCreated = useCallback(
    (_: WatchedToken) =>
      void invalidateAllWorkloadDeviceJoinTokens(queryClient),
    [queryClient],
  );

  const close = (next: boolean) => {
    onOpenChange(next);
    if (!next) {
      // A fresh sheet next time: a token shown once must not reappear.
      setStarted(false);
      setGrantError(null);
    }
  };

  return (
    <Sheet open={open} onOpenChange={close}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            Issue bootstrap token
          </SheetTitle>
          <SheetDescription>
            Enroll a headless box, like a VPS or sandbox, as {agent.name}. Linux
            only for now.
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          {grants.isPending ? (
            <Text muted small>
              Checking this agent&rsquo;s permissions…
            </Text>
          ) : grants.isError ? (
            <div role="alert" className="space-y-2">
              <Text small>Could not check this agent&rsquo;s permissions.</Text>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void grants.refetch()}
              >
                Try again
              </Button>
            </div>
          ) : !canSync ? (
            // The server refuses a token for an agent without device sync, so
            // the fix is offered here rather than left to a failed request.
            <div className="border-border space-y-3 border p-4">
              <Text small className="font-medium">
                This agent cannot enroll devices yet
              </Text>
              <Text muted small>
                A device checks in for its policy as the agent, which the agent
                must be allowed to do.
              </Text>
              <Button
                size="sm"
                disabled={granting}
                onClick={() =>
                  void grant(
                    deviceEnrollmentPolicyGrants({
                      organizationId: organization.id,
                    }),
                  )
                }
              >
                {granting ? "Allowing…" : "Allow device enrollment"}
              </Button>
              {grantError && (
                <Text role="alert" small>
                  {grantError}
                </Text>
              )}
            </div>
          ) : (
            <>
              <DeviceEnrollmentFields
                projectId={hooksProjectId}
                onProjectChange={setProjectId}
                lifetimeSeconds={lifetime}
                onLifetimeChange={setLifetime}
                disabled={started}
                hooksNote={
                  hooksProject !== undefined && !hooksAccepted ? (
                    <div className="border-border space-y-2 border p-3">
                      <Text muted small>
                        This agent is not allowed to send hooks to{" "}
                        {hooksProject.name}, so the box&rsquo;s hook events are
                        refused until it is. Enrolling still works.
                      </Text>
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={granting}
                        onClick={() =>
                          void grant([
                            hooksIngestGrant(organization.id, hooksProject.id),
                          ])
                        }
                      >
                        {granting
                          ? "Allowing…"
                          : `Allow hooks to ${hooksProject.name}`}
                      </Button>
                      {grantError && (
                        <Text role="alert" small>
                          {grantError}
                        </Text>
                      )}
                    </div>
                  ) : null
                }
              />
              {started && hooksProject ? (
                <DeviceSetupPanel
                  agentId={agent.id}
                  hooksProject={hooksProject}
                  lifetimeSeconds={lifetime}
                  onTokenCreated={onTokenCreated}
                />
              ) : null}
            </>
          )}
        </div>
        <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
          <Button variant="secondary" onClick={() => close(false)}>
            {started ? "Done" : "Cancel"}
          </Button>
          {!started && (
            <Button
              disabled={!canSync || hooksProject === undefined}
              onClick={() => setStarted(true)}
            >
              Continue
            </Button>
          )}
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
