import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import {
  DangerSettingsSection,
  ResourceListPage,
} from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { MoreActions } from "@/components/ui/MoreActions";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Column, Table } from "@/components/ui/Table";
import { TablePagination } from "@/components/ui/TablePagination";
import { usePagedRows } from "@/components/ui/TablePagination/usePagedRows";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import { admissionMatches } from "./search";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import {
  invalidateAllWorkloadIdentities,
  useWorkloadIdentities,
} from "@gram/client/react-query/workloadIdentities.js";
import { useAdmitWorkloadSubjectMutation } from "@gram/client/react-query/admitWorkloadSubject.js";
import { useAgents } from "@gram/client/react-query/agents.js";
import { useUpdateWorkloadIssuerMutation } from "@gram/client/react-query/updateWorkloadIssuer.js";
import { useUpdateWorkloadSubjectMutation } from "@gram/client/react-query/updateWorkloadSubject.js";
import { useWithdrawWorkloadIssuerMutation } from "@gram/client/react-query/withdrawWorkloadIssuer.js";
import { useWithdrawWorkloadSubjectMutation } from "@gram/client/react-query/withdrawWorkloadSubject.js";
import { WithdrawIssuerDialog } from "./WithdrawIssuerDialog";
import { RemoveSubjectDialog } from "./RemoveSubjectDialog";
import { Pencil, Plus } from "lucide-react";
import {
  RegisterIssuerSheet,
  type RegisterIssuerValues,
} from "./RegisterIssuerSheet";
import { changedIssuerFields } from "./issuerEdit";
import { changedAdmissionFields } from "./admissionEdit";
import {
  AdmitSubjectSheet,
  type AdmitSubjectInitialValues,
  type AdmitSubjectValues,
} from "./AdmitSubjectSheet";
import { useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useMemo, useState } from "react";
import { Navigate, useParams } from "react-router";
import { toast } from "sonner";

const MACHINES_PAGE_SIZE = 10;

// The identifiers sit on labeled lines of their own, apart from the
// description, because they are what an administrator copies into the
// platform's console.
function IssuerIdentifiers({
  issuer,
}: {
  issuer: WorkloadIssuer;
}): JSX.Element {
  return (
    <dl className="mb-6 grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1">
      <dt>
        <Text muted small>
          Issuer URL
        </Text>
      </dt>
      <dd>
        <Text small className="font-mono break-all">
          {issuer.issuer}
        </Text>
      </dd>
      <dt>
        <Text muted small>
          Keys URL
        </Text>
      </dt>
      <dd>
        <Text small className="font-mono break-all">
          {issuer.jwksUri}
        </Text>
      </dd>
    </dl>
  );
}

function admissionInitialValues(
  admission: WorkloadAdmission,
): AdmitSubjectInitialValues {
  return {
    subject: admission.subject,
    name: admission.name,
    tags: admission.tags,
    agentId: admission.agentId,
    agentName: admission.agentName,
  };
}

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
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [admitOpen, setAdmitOpen] = useState(false);
  const [withdrawOpen, setWithdrawOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [removing, setRemoving] = useState<WorkloadAdmission | null>(null);
  // Held apart from the open flag so the sheet keeps the machine it is editing
  // while it animates closed.
  const [editingAdmission, setEditingAdmission] =
    useState<WorkloadAdmission | null>(null);
  const [editAdmissionOpen, setEditAdmissionOpen] = useState(false);
  const { data, isPending, isError, refetch } = useWorkloadIdentities({});
  // throwOnError because the whole agents service 404s where the agent
  // management rollout is off, and the global query policy suppresses only 401
  // and 403 — left to throw it takes this page down with it.
  const agentsQuery = useAgents({}, undefined, { throwOnError: false });

  const issuer = useMemo(
    () => data?.issuers?.find((candidate) => candidate.id === issuerId),
    [data, issuerId],
  );

  // Memoized so the edit sheet resets only when the stored issuer changes, not
  // on every render.
  const editValues = useMemo<RegisterIssuerValues | undefined>(
    () =>
      issuer && {
        name: issuer.name,
        description: issuer.description,
        issuer: issuer.issuer,
        jwksUri: issuer.jwksUri,
        tags: issuer.tags,
      },
    [issuer],
  );

  // Memoized so the edit sheet resets only when the machine being edited
  // changes, not on every render.
  const admissionEditValues = useMemo(
    () => editingAdmission && admissionInitialValues(editingAdmission),
    [editingAdmission],
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

  const { page, pageRows, setPage } = usePagedRows({
    rows: visibleAdmissions,
    pageSize: MACHINES_PAGE_SIZE,
    resetOn: [issuerId, search],
  });

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
      ? "Agents are unavailable right now, so access can't be allowed."
      : (agentsQuery.data ?? []).length === 0
        ? "Create an agent first: every machine acts under an agent's policy."
        : agents.length === 0
          ? "Every agent is suspended, revoked or waiting for a new owner. Reactivate or reassign one to allow access."
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

  const updateIssuer = useUpdateWorkloadIssuerMutation({
    onSuccess: async () => {
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setEditOpen(false);
      toast.success("Platform updated");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update the platform",
      );
    },
  });

  const updateSubject = useUpdateWorkloadSubjectMutation({
    onSuccess: async () => {
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setEditAdmissionOpen(false);
      toast.success("Access updated");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to update access",
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
      setRemoving(null);
      toast.success("Machine removed");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to remove the machine",
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
    return <Navigate to={orgRoutes.workloadIssuers.href()} replace />;
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
        </Stack>
      ),
    },
    {
      key: "tags",
      header: "Tags",
      width: "200px",
      render: (admission) =>
        admission.tags.length > 0 ? (
          <div className="flex flex-wrap gap-1">
            {admission.tags.map((tag) => (
              <Badge key={tag} variant="information">
                {tag}
              </Badge>
            ))}
          </div>
        ) : (
          <Text muted>None</Text>
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
      width: "56px",
      render: (admission) => (
        <RequireScope scope="workload:write" level="component">
          <MoreActions
            triggerAriaLabel={`Actions for ${admission.name || admission.subject}`}
            actions={[
              {
                label: "Edit",
                icon: "pencil",
                onClick: () => {
                  setEditingAdmission(admission);
                  setEditAdmissionOpen(true);
                },
              },
              {
                label: "Remove",
                icon: "trash",
                destructive: true,
                disabled: withdrawSubject.isPending,
                onClick: () => setRemoving(admission),
              },
            ]}
          />
        </RequireScope>
      ),
    },
  ];

  // Loading first: an empty table during the first fetch would read as "no
  // machines match" before anything has loaded.
  let machinesSection: ReactNode;
  if (isPending) {
    machinesSection = <SkeletonTable />;
  } else if (admissions.length === 0) {
    machinesSection = (
      <InlineEmptyState
        icon="cpu"
        heading="No machines allowed from this platform"
        description="Trusting a platform allows nothing on its own. Allow access for a machine so it can exchange its identity token for a Gram session."
      />
    );
  } else {
    machinesSection = (
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
          data={pageRows}
          rowKey={(row) => row.id}
          noResultsMessage={<Text>No machines match that search.</Text>}
        />
        <TablePagination
          page={page}
          pageSize={MACHINES_PAGE_SIZE}
          totalItems={visibleAdmissions.length}
          onPageChange={setPage}
        />
      </>
    );
  }

  const editButton = (
    <RequireScope scope="workload:write" level="component">
      <Button
        size="sm"
        variant="secondary"
        onClick={() => setEditOpen(true)}
        disabled={issuer === undefined}
      >
        <Button.LeftIcon>
          <Pencil className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Edit</Button.Text>
      </Button>
    </RequireScope>
  );

  const allowButton = (
    <RequireScope scope="workload:write" level="component">
      <Button
        size="sm"
        onClick={() => setAdmitOpen(true)}
        // The sheet is mounted only once the platform has loaded, so the button
        // waits for it too rather than opening nothing.
        disabled={issuer === undefined || agents.length === 0}
      >
        <Button.LeftIcon>
          <Plus className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Allow access</Button.Text>
      </Button>
    </RequireScope>
  );

  const stopTrustingSection = (
    <RequireScope scope="workload:write" level="component">
      <div className="mt-10">
        <DangerSettingsSection>
          <DangerSettingsSection.Header>
            <DangerSettingsSection.Title>
              Stop trusting this platform
            </DangerSettingsSection.Title>
            <DangerSettingsSection.Description>
              Removes the platform and every machine allowed under it. Its
              identity tokens are no longer accepted, though sessions already
              issued are not revoked.
            </DangerSettingsSection.Description>
          </DangerSettingsSection.Header>
          <DangerSettingsSection.Panel>
            <DangerSettingsSection.Body>
              <div>
                <Button
                  variant="destructive-primary"
                  onClick={() => setWithdrawOpen(true)}
                  disabled={withdrawIssuer.isPending}
                >
                  <Button.Text>Stop trusting</Button.Text>
                </Button>
              </div>
            </DangerSettingsSection.Body>
          </DangerSettingsSection.Panel>
        </DangerSettingsSection>
      </div>
    </RequireScope>
  );

  const handleEdit = (
    values: RegisterIssuerValues,
    baseline: RegisterIssuerValues | undefined,
  ) => {
    if (issuer === undefined || baseline === undefined) return;
    updateIssuer.mutate({
      request: {
        updateWorkloadIssuerForm: {
          id: issuer.id,
          ...changedIssuerFields(baseline, values),
        },
      },
    });
  };

  const handleEditAdmission = (values: AdmitSubjectValues) => {
    if (editingAdmission === null || admissionEditValues === null) return;
    updateSubject.mutate({
      request: {
        updateWorkloadSubjectForm: {
          id: editingAdmission.id,
          ...changedAdmissionFields(admissionEditValues, values),
        },
      },
    });
  };

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
        },
      },
    });
  };

  return (
    <ResourceListPage
      primaryAction={
        <Stack direction="horizontal" gap={2}>
          {editButton}
          {allowButton}
        </Stack>
      }
      title={issuer?.name ?? "Trusted platform"}
      description={issuer?.description.trim() || undefined}
      belowHeader={issuer && <IssuerIdentifiers issuer={issuer} />}
    >
      {allowUnavailableReason !== null && (
        <Text muted small className="mb-4">
          {allowUnavailableReason}
        </Text>
      )}
      {machinesSection}
      {issuer && stopTrustingSection}
      <RemoveSubjectDialog
        admission={removing}
        onOpenChange={(open) => {
          if (!open) setRemoving(null);
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

      {editValues && (
        <RegisterIssuerSheet
          open={editOpen}
          onOpenChange={setEditOpen}
          onSubmit={handleEdit}
          isPending={updateIssuer.isPending}
          initial={editValues}
        />
      )}

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

      {issuer && admissionEditValues && (
        <AdmitSubjectSheet
          // Remounted per machine: opening a different machine's edit changes
          // the values in the same render as it opens the sheet, which the
          // sheet's reset-on-close never sees.
          key={editingAdmission?.id}
          open={editAdmissionOpen}
          onOpenChange={setEditAdmissionOpen}
          onSubmit={handleEditAdmission}
          isPending={updateSubject.isPending}
          issuer={issuer}
          agents={agents}
          initial={admissionEditValues}
        />
      )}
    </ResourceListPage>
  );
}
