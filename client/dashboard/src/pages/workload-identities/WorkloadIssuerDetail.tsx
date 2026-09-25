import { InlineEmptyState } from "@/components/inline-empty-state";
import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Stack } from "@/components/ui/Stack";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import {
  invalidateAllWorkloadIdentities,
  useWorkloadIdentities,
} from "@gram/client/react-query/workloadIdentities.js";
import { useAdmitWorkloadSubjectMutation } from "@gram/client/react-query/admitWorkloadSubject.js";
import { useAgents } from "@gram/client/react-query/agents.js";
import { useWithdrawWorkloadIssuerMutation } from "@gram/client/react-query/withdrawWorkloadIssuer.js";
import { useWithdrawWorkloadSubjectMutation } from "@gram/client/react-query/withdrawWorkloadSubject.js";
import { Plus } from "lucide-react";
import { useState } from "react";
import {
  AdmitSubjectDialog,
  type AdmitSubjectValues,
} from "./AdmitSubjectDialog";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { Navigate, useParams } from "react-router";
import { toast } from "sonner";

/**
 * One issuer, and the subjects admitted under it.
 *
 * The policy list already returns both halves, so this filters rather than
 * fetching: an admission names the issuer row it was written against, which is
 * what makes "this issuer's workloads" a well-defined set even where two issuers
 * share a URL across tiers.
 */
export function WorkloadIssuerDetailPage(): JSX.Element {
  return (
    <RequireScope scope={["workload:read", "workload:write"]} level="page">
      <IssuerDetail />
    </RequireScope>
  );
}

function IssuerDetail(): JSX.Element {
  const { issuerId = "" } = useParams<{ issuerId: string }>();
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [admitOpen, setAdmitOpen] = useState(false);
  const [withdrawOpen, setWithdrawOpen] = useState(false);
  const { data, isPending } = useWorkloadIdentities({});
  // throwOnError because the whole agents service 404s where the agent
  // management rollout is off, and the global query policy suppresses only 401
  // and 403 — left to throw it takes this page down with it.
  const agentsQuery = useAgents({}, undefined, { throwOnError: false });

  const issuer = useMemo(
    () => data?.issuers?.find((candidate) => candidate.id === issuerId),
    [data, issuerId],
  );

  const admissions = useMemo(
    () =>
      (data?.admissions ?? []).filter(
        (admission) => admission.workloadIssuerId === issuerId,
      ),
    [data, issuerId],
  );

  const agents = useMemo(
    () =>
      (agentsQuery.data ?? [])
        // A suspended or revoked agent contributes no policy, so a machine
        // assigned to one authenticates and can reach nothing.
        .filter((agent) => agent.lifecycle === "active")
        .map((agent) => ({ id: agent.id, name: agent.name })),
    [agentsQuery.data],
  );

  const admitSubject = useAdmitWorkloadSubjectMutation({
    onSuccess: async () => {
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setAdmitOpen(false);
      toast.success("Machine allowed");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to allow the machine",
      );
    },
  });

  const withdrawIssuer = useWithdrawWorkloadIssuerMutation({
    onSuccess: async () => {
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setWithdrawOpen(false);
      toast.success("Platform withdrawn");
      // No redirect here: the issuer leaves the policy the refetch returns, and
      // the guard below sends this page back to the list on its own.
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to withdraw the platform",
      );
    },
  });

  const withdrawSubject = useWithdrawWorkloadSubjectMutation({
    onSuccess: async () => {
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      toast.success("Workload withdrawn");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to withdraw the workload",
      );
    },
  });

  // Withdrawn elsewhere, or a stale link. Send them back to the list rather than
  // rendering a page about a row that is gone.
  if (!isPending && issuer === undefined) {
    return <Navigate to={routes.workloadIssuers.href()} replace />;
  }

  const columns: Column<WorkloadAdmission>[] = [
    {
      key: "subject",
      header: "Subject",
      render: (admission) => (
        <Stack gap={1}>
          <Text className="font-mono text-xs break-all">
            {admission.subject}
          </Text>
          {admission.name.length > 0 && (
            <Text muted small>
              {admission.name}
            </Text>
          )}
        </Stack>
      ),
    },
    {
      key: "agent",
      header: "Agent",
      width: "180px",
      render: (admission) =>
        admission.agentName.length > 0 ? (
          <Text>{admission.agentName}</Text>
        ) : (
          <Text muted>None assigned</Text>
        ),
    },
    {
      key: "tier",
      header: "Admitted by",
      width: "130px",
      render: (admission) => (
        <Text>{admission.projectId ? "This project" : "Organization"}</Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "120px",
      render: (admission) => (
        <RequireScope scope="workload:write" level="component">
          <Button
            size="sm"
            variant="tertiary"
            disabled={withdrawSubject.isPending}
            onClick={() =>
              withdrawSubject.mutate({ request: { id: admission.id } })
            }
          >
            <Button.Text>Withdraw</Button.Text>
          </Button>
        </RequireScope>
      ),
    },
  ];

  const allowButton = (
    <RequireScope scope="workload:write" level="component">
      <Button
        size="sm"
        onClick={() => setAdmitOpen(true)}
        disabled={agents.length === 0}
      >
        <Button.LeftIcon>
          <Plus className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Allow a machine</Button.Text>
      </Button>
    </RequireScope>
  );

  const withdrawButton = (
    <RequireScope scope="workload:write" level="component">
      <Button
        size="sm"
        variant="tertiary"
        onClick={() => setWithdrawOpen(true)}
        disabled={withdrawIssuer.isPending}
      >
        <Button.Text>Stop trusting</Button.Text>
      </Button>
    </RequireScope>
  );

  const handleAllow = (values: AdmitSubjectValues) => {
    const label = values.name.trim();
    admitSubject.mutate({
      request: {
        admitWorkloadSubjectForm: {
          issuer: values.issuer,
          subject: values.subject.trim(),
          matchKind: values.matchKind,
          name: label.length > 0 ? label : undefined,
          agentId: values.agentId,
        },
      },
    });
  };

  return (
    <ResourceListPage
      primaryAction={
        <Stack direction="horizontal" gap={2} align="center">
          {withdrawButton}
          {allowButton}
        </Stack>
      }
      title={issuer?.name ?? "Trusted platform"}
      description={
        issuer
          ? `${issuer.issuer} — the identifier an assertion's iss claim must carry. Keys at ${issuer.jwksUri}.`
          : undefined
      }
    >
      {issuer && (
        <Badge variant="neutral" background className="mb-6">
          {issuer.projectId === "" ? "Organization" : "This project"}
        </Badge>
      )}

      {admissions.length === 0 && !isPending ? (
        <InlineEmptyState
          icon="cpu"
          heading="No machines allowed from this platform"
          description="Trusting a platform allows nothing on its own. Allow a machine so it can exchange its identity token for a Gram session."
        />
      ) : (
        <Table columns={columns} data={admissions} rowKey={(row) => row.id} />
      )}
      <Dialog open={withdrawOpen} onOpenChange={setWithdrawOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Stop trusting this platform?</Dialog.Title>
            <Dialog.Description>
              {admissions.length === 0
                ? "Gram will stop accepting this platform's identity tokens. Nothing is allowed under it, so nothing else changes."
                : `Gram will stop accepting this platform's identity tokens, and the ${
                    admissions.length === 1
                      ? "machine allowed under it"
                      : `${admissions.length} machines allowed under it`
                  } will stop authenticating. Existing sessions are not revoked by this.`}
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="tertiary"
              onClick={() => setWithdrawOpen(false)}
              disabled={withdrawIssuer.isPending}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={withdrawIssuer.isPending || issuer === undefined}
              onClick={() => {
                if (issuer !== undefined) {
                  withdrawIssuer.mutate({ request: { id: issuer.id } });
                }
              }}
            >
              <Button.Text>
                {withdrawIssuer.isPending ? "Withdrawing…" : "Stop trusting"}
              </Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>

      {issuer && (
        <AdmitSubjectDialog
          open={admitOpen}
          onOpenChange={setAdmitOpen}
          onSubmit={handleAllow}
          isPending={admitSubject.isPending}
          // Scoped to this platform: the page is already about one, so offering
          // a choice of issuer here would be a second way to say where you are.
          issuers={[issuer]}
          agents={agents}
        />
      )}
    </ResourceListPage>
  );
}
