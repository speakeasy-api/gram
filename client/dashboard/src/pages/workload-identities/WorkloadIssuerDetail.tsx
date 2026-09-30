import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { admissionMatches } from "./search";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import {
  invalidateAllWorkloadIdentities,
  useWorkloadIdentities,
} from "@gram/client/react-query/workloadIdentities.js";
import { useAdmitWorkloadSubjectMutation } from "@gram/client/react-query/admitWorkloadSubject.js";
import { useAgents } from "@gram/client/react-query/agents.js";
import { useWithdrawWorkloadIssuerMutation } from "@gram/client/react-query/withdrawWorkloadIssuer.js";
import { useWithdrawWorkloadSubjectMutation } from "@gram/client/react-query/withdrawWorkloadSubject.js";
import { WithdrawIssuerDialog } from "./WithdrawIssuerDialog";
import { WithdrawSubjectDialog } from "./WithdrawSubjectDialog";
import { Plus } from "lucide-react";
import {
  AdmitSubjectSheet,
  type AdmitSubjectValues,
} from "./AdmitSubjectSheet";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
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
// The operator's description leads when there is one; the identifiers follow
// because they are what an administrator copies into the platform's console.
function issuerSummary(issuer: WorkloadIssuer): string {
  const identifiers = `Issuer ${issuer.issuer}, keys at ${issuer.jwksUri}.`;
  const description = issuer.description.trim();
  if (description === "") {
    return identifiers;
  }
  // An operator's description need not end in punctuation, and without it the
  // identifiers would read as part of the same sentence.
  const separator = /[.!?]$/.test(description) ? " " : ". ";
  return `${description}${separator}${identifiers}`;
}

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
  const [search, setSearch] = useState("");
  const [withdrawing, setWithdrawing] = useState<WorkloadAdmission | null>(
    null,
  );
  const { data, isPending, isError, refetch } = useWorkloadIdentities({});
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

  const visibleAdmissions = useMemo(
    () => admissions.filter((admission) => admissionMatches(admission, search)),
    [admissions, search],
  );

  const agents = useMemo(
    () =>
      (agentsQuery.data ?? [])
        // A suspended or revoked agent contributes no policy, so a machine
        // assigned to one authenticates and can reach nothing. An agent waiting
        // for a new owner is refused everywhere else agents are picked, so it is
        // refused here too.
        .filter(
          (agent) =>
            agent.lifecycle === "active" && !agent.ownerReassignmentRequiredAt,
        )
        .map((agent) => ({ id: agent.id, name: agent.name })),
    [agentsQuery.data],
  );

  const allowUnavailableReason = agentsQuery.isPending
    ? null
    : agentsQuery.isError
      ? "Agents are unavailable right now, so no machine can be allowed."
      : (agentsQuery.data ?? []).length === 0
        ? "Create an agent first: every machine acts under an agent's policy."
        : agents.length === 0
          ? "Every agent is suspended, revoked or waiting for a new owner. Reactivate or reassign one to allow a machine."
          : null;

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
      setWithdrawing(null);
      toast.success("Machine withdrawn");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to withdraw the machine",
      );
    },
  });

  // A failed load says nothing about whether the platform exists, so it gets a
  // retry rather than the redirect below, which would otherwise hide the error
  // behind the list.
  if (isError) {
    return (
      <ResourceListPage title="Trusted platform">
        <InlineEmptyState
          icon="triangle-alert"
          heading="Couldn't load this platform"
          description="The trust policy failed to load. Try again in a moment."
          action={
            <Button
              size="sm"
              variant="secondary"
              onClick={() => void refetch()}
            >
              <Button.Text>Try again</Button.Text>
            </Button>
          }
        />
      </ResourceListPage>
    );
  }

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
          {/* The label leads when there is one: it is the operator's own name
              for the machine, and reads faster than the subject beneath it. */}
          {admission.name.length > 0 && (
            <Text className="font-medium">{admission.name}</Text>
          )}
          <Text className="font-mono text-xs break-all">
            {admission.subject}
          </Text>
          {!admission.wildcardActive && (
            <Text small destructive>
              Inactive: this platform does not permit wildcard rules, so this
              rule matches nothing.
            </Text>
          )}
          {admission.tags.length > 0 && (
            <div className="flex flex-wrap gap-1">
              {admission.tags.map((tag) => (
                <Badge key={tag} variant="information">
                  {tag}
                </Badge>
              ))}
            </div>
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
      key: "actions",
      header: "",
      width: "120px",
      render: (admission) => (
        <RequireScope scope="workload:write" level="component">
          <Button
            size="sm"
            variant="tertiary"
            disabled={withdrawSubject.isPending}
            onClick={() => setWithdrawing(admission)}
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
          tags: values.tags,
          agentId: values.agentId,
          // The admission is written at the platform's own tier, which the
          // server requires to match the issuer it names.
          projectScoped: issuer !== undefined && issuer.projectId !== "",
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
      description={issuer ? issuerSummary(issuer) : undefined}
    >
      {allowUnavailableReason !== null && (
        <Text muted small className="mb-4">
          {allowUnavailableReason}
        </Text>
      )}
      {admissions.length === 0 && !isPending ? (
        <InlineEmptyState
          icon="cpu"
          heading="No machines allowed from this platform"
          description="Trusting a platform allows nothing on its own. Allow a machine so it can exchange its identity token for a Gram session."
        />
      ) : (
        <>
          <Page.Toolbar className="mb-4">
            <Page.Toolbar.Search
              className="w-full"
              value={search}
              onChange={setSearch}
              placeholder="Search subject, label, tag or agent…"
            />
          </Page.Toolbar>
          <Table
            columns={columns}
            data={visibleAdmissions}
            rowKey={(row) => row.id}
            noResultsMessage={<Text>No machines match that search.</Text>}
          />
        </>
      )}
      <WithdrawSubjectDialog
        admission={withdrawing}
        onOpenChange={(open) => {
          if (!open) setWithdrawing(null);
        }}
        onConfirm={(admission) =>
          withdrawSubject.mutate({ request: { id: admission.id } })
        }
        isPending={withdrawSubject.isPending}
      />
      <WithdrawIssuerDialog
        issuer={issuer}
        open={withdrawOpen}
        onOpenChange={setWithdrawOpen}
        onConfirm={(target) =>
          withdrawIssuer.mutate({ request: { id: target.id } })
        }
        isPending={withdrawIssuer.isPending}
        machineCount={admissions.length}
      />

      {issuer && (
        <AdmitSubjectSheet
          open={admitOpen}
          onOpenChange={setAdmitOpen}
          onSubmit={handleAllow}
          isPending={admitSubject.isPending}
          issuer={issuer}
          agents={agents}
        />
      )}
    </ResourceListPage>
  );
}
