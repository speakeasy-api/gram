import { InlineEmptyState } from "@/components/inline-empty-state";
import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Stack } from "@/components/ui/Stack";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { useAdmitWorkloadSubjectMutation } from "@gram/client/react-query/admitWorkloadSubject.js";
import { useAgents } from "@gram/client/react-query/agents.js";
import { useRegisterWorkloadIssuerMutation } from "@gram/client/react-query/registerWorkloadIssuer.js";
import { useWithdrawWorkloadIssuerMutation } from "@gram/client/react-query/withdrawWorkloadIssuer.js";
import { useWithdrawWorkloadSubjectMutation } from "@gram/client/react-query/withdrawWorkloadSubject.js";
import {
  invalidateAllWorkloadIdentities,
  useWorkloadIdentities,
} from "@gram/client/react-query/workloadIdentities.js";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";
import {
  AdmitSubjectDialog,
  type AdmitSubjectValues,
} from "./AdmitSubjectDialog";
import {
  RegisterIssuerDialog,
  type RegisterIssuerValues,
} from "./RegisterIssuerDialog";

export function WorkloadIdentitiesRoot(): JSX.Element {
  return <Outlet />;
}

export function WorkloadIdentitiesPage(): JSX.Element {
  // Gated from outside the template so the policy is never fetched for someone
  // who may not read it: the admitted set discloses which machines the
  // organization recognises.
  return (
    <RequireScope scope="workload:read" level="page">
      <WorkloadIdentitiesOverview />
    </RequireScope>
  );
}

// Returns null when a workload can be admitted, or the reason it cannot.
function admitPrecondition(
  issuerCount: number,
  agentCount: number,
  activeAgentCount: number,
  agentsFailed: boolean,
): string | null {
  if (issuerCount === 0) {
    return "Trust an issuer before admitting a workload: a subject is only meaningful under the issuer that asserts it.";
  }
  if (agentsFailed) {
    return "Agents could not be listed, so there is nothing to assign. Agent management is not enabled for this organization.";
  }
  if (agentCount === 0) {
    return "Create an agent first. An admitted workload inherits its policy from an agent, and one admitted without an agent is refused when it tries to authenticate.";
  }
  // Only active agents are assignable, so an organization can hold agents and
  // still have none to offer. Saying "create an agent" there sends someone to
  // make a second one instead of reactivating the one they have.
  if (activeAgentCount === 0) {
    return "Every agent is suspended or revoked. A workload inherits its policy from an agent, so reactivate one before admitting a workload.";
  }
  return null;
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

function WorkloadIdentitiesOverview(): JSX.Element {
  const queryClient = useQueryClient();
  const { data, isPending } = useWorkloadIdentities({});
  // throwOnError because the whole agents service 404s when the agent
  // management rollout is off for the organization, and the global query policy
  // only suppresses 401 and 403. Left to throw it takes this page down with it,
  // even though the trust policy itself loaded: listing and withdrawing do not
  // need an agent, only admitting does.
  const agentsQuery = useAgents({}, undefined, { throwOnError: false });

  const [registerOpen, setRegisterOpen] = useState(false);
  const [admitOpen, setAdmitOpen] = useState(false);

  const issuers = useMemo(() => data?.issuers ?? [], [data]);
  const admissions = useMemo(() => data?.admissions ?? [], [data]);
  const agents = useMemo(
    () =>
      (agentsQuery.data ?? [])
        // A suspended or revoked agent contributes no policy until it is active
        // again, so admitting a workload against one produces a machine that
        // authenticates and can reach nothing. Offer only agents that can back
        // the assignment. Project-bound agents stay: the assignment's foreign
        // key is organization-scoped, so they are assignable.
        .filter((agent) => agent.lifecycle === "active")
        .map((agent) => ({
          id: agent.id,
          name: agent.name,
        })),
    [agentsQuery.data],
  );

  // Every write returns the whole policy, so the cache is refreshed from the
  // response rather than patched: the page cannot show a stale table.
  const refresh = async () => {
    await invalidateAllWorkloadIdentities(queryClient, { refetchType: "all" });
  };

  const registerIssuer = useRegisterWorkloadIssuerMutation({
    onSuccess: async () => {
      await refresh();
      setRegisterOpen(false);
      toast.success("Issuer trusted");
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to trust the issuer"));
    },
  });

  const withdrawIssuer = useWithdrawWorkloadIssuerMutation({
    onSuccess: async () => {
      await refresh();
      toast.success(
        "Issuer withdrawn, along with the subjects admitted under it",
      );
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to withdraw the issuer"));
    },
  });

  const admitSubject = useAdmitWorkloadSubjectMutation({
    onSuccess: async () => {
      await refresh();
      setAdmitOpen(false);
      toast.success("Workload admitted");
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to admit the workload"));
    },
  });

  const withdrawSubject = useWithdrawWorkloadSubjectMutation({
    onSuccess: async () => {
      await refresh();
      toast.success("Workload withdrawn");
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to withdraw the workload"));
    },
  });

  const handleRegister = (values: RegisterIssuerValues) => {
    registerIssuer.mutate({
      request: {
        registerWorkloadIssuerForm: {
          name: values.name.trim(),
          issuer: values.issuer.trim(),
          jwksUri: values.jwksUri.trim(),
          allowWildcardAdmission: values.allowWildcardAdmission,
        },
      },
    });
  };

  const handleAdmit = (values: AdmitSubjectValues) => {
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

  const registerButton = (
    <RequireScope scope="workload:write" level="component">
      <Button size="sm" onClick={() => setRegisterOpen(true)}>
        <Button.LeftIcon>
          <Plus />
        </Button.LeftIcon>
        <Button.Text>Trust an issuer</Button.Text>
      </Button>
    </RequireScope>
  );

  // Admitting needs an agent to assign, because a subject admitted without one
  // is refused at the token endpoint. Say which precondition is missing rather
  // than offering a control that fails on submit.
  const admitBlockedReason = admitPrecondition(
    issuers.length,
    agentsQuery.data?.length ?? 0,
    agents.length,
    agentsQuery.isError,
  );

  const admitButton = (
    <RequireScope scope="workload:write" level="component">
      <Button
        size="sm"
        variant="secondary"
        onClick={() => setAdmitOpen(true)}
        disabled={admitBlockedReason !== null}
      >
        <Button.LeftIcon>
          <Plus />
        </Button.LeftIcon>
        <Button.Text>Admit a workload</Button.Text>
      </Button>
    </RequireScope>
  );

  const issuerColumns: Column<WorkloadIssuer>[] = [
    {
      key: "name",
      header: "Name",
      width: "180px",
      render: (issuer) => <Text className="font-medium">{issuer.name}</Text>,
    },
    {
      key: "issuer",
      header: "Issuer",
      render: (issuer) => (
        <Text className="font-mono text-xs">{issuer.issuer}</Text>
      ),
    },
    {
      key: "tier",
      header: "Trusted by",
      width: "130px",
      render: (issuer) => (
        <Text>{issuer.projectId ? "This project" : "Organization"}</Text>
      ),
    },
    {
      key: "wildcard",
      header: "Wildcards",
      width: "120px",
      render: (issuer) =>
        issuer.allowWildcardAdmission ? (
          <Badge variant="warning" background>
            Allowed
          </Badge>
        ) : (
          <Badge variant="neutral" background>
            Off
          </Badge>
        ),
    },
    {
      key: "actions",
      header: "",
      width: "120px",
      render: (issuer) => (
        <RequireScope scope="workload:write" level="component">
          <Button
            size="sm"
            variant="tertiary"
            disabled={withdrawIssuer.isPending}
            onClick={() =>
              withdrawIssuer.mutate({ request: { id: issuer.id } })
            }
          >
            <Button.Text>Withdraw</Button.Text>
          </Button>
        </RequireScope>
      ),
    },
  ];

  const admissionColumns: Column<WorkloadAdmission>[] = [
    {
      key: "subject",
      header: "Subject",
      render: (admission) => (
        <Stack gap={1}>
          <Text className="font-mono text-xs">{admission.subject}</Text>
          {admission.name.length > 0 && (
            <Text muted small>
              {admission.name}
            </Text>
          )}
        </Stack>
      ),
    },
    {
      key: "match",
      header: "Match",
      width: "140px",
      render: (admission) => <MatchCell admission={admission} />,
    },
    {
      key: "issuer",
      header: "Issuer",
      width: "160px",
      render: (admission) => <Text>{admission.issuerName}</Text>,
    },
    {
      key: "agent",
      header: "Agent",
      width: "160px",
      render: (admission) => <AgentCell admission={admission} />,
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

  return (
    <>
      <ResourceListPage
        title="Workload Identities"
        description="The machines this organization recognises as its own. An issuer vouches for a workload, an admitted subject is the grant of access, and the assigned agent supplies the policy the workload acts under."
        primaryAction={registerButton}
        isLoading={isPending}
        isEmpty={issuers.length === 0}
        empty={{
          icon: "key-round",
          heading: "No trusted issuers yet",
          description:
            "Trust the platform that issues your workloads' tokens, then admit the subjects it asserts.",
          action: registerButton,
        }}
      >
        <Table
          columns={issuerColumns}
          data={issuers}
          rowKey={(row) => row.id}
          noResultsMessage={<Text>No trusted issuers</Text>}
        />

        {/* The page header is rendered once by the template above; this is a
            plain section heading. */}
        <Stack gap={4} className="mt-8 mb-6">
          <Stack
            direction="horizontal"
            justify="space-between"
            align="center"
            gap={4}
          >
            <div className="min-w-0">
              <Heading variant="h4" className="mb-2">
                Admitted Workloads
              </Heading>
              <Text muted small className="max-w-2xl">
                Each row is a subject one of the issuers above must assert, and
                the agent it inherits its policy from.
              </Text>
            </div>
            {admitButton}
          </Stack>

          {admitBlockedReason !== null && (
            <Text muted small className="max-w-2xl">
              {admitBlockedReason}
            </Text>
          )}

          {admissions.length === 0 ? (
            <InlineEmptyState
              icon="bot"
              heading="No workloads admitted"
              description="Admit a subject to let a machine exchange its token for a Gram session."
              action={admitButton}
            />
          ) : (
            <Table
              columns={admissionColumns}
              data={admissions}
              rowKey={(row) => row.id}
            />
          )}
        </Stack>
      </ResourceListPage>

      <RegisterIssuerDialog
        open={registerOpen}
        onOpenChange={setRegisterOpen}
        onSubmit={handleRegister}
        isPending={registerIssuer.isPending}
      />

      <AdmitSubjectDialog
        open={admitOpen}
        onOpenChange={setAdmitOpen}
        onSubmit={handleAdmit}
        isPending={admitSubject.isPending}
        issuers={issuers}
        agents={agents}
      />
    </>
  );
}

function MatchCell({
  admission,
}: {
  admission: WorkloadAdmission;
}): JSX.Element {
  if (admission.matchKind !== "wildcard") {
    return (
      <Badge variant="neutral" background>
        Exact
      </Badge>
    );
  }

  // A rule written while the issuer permitted wildcards stays stored after the
  // permission is cleared, and matches nothing. Showing it as an ordinary
  // wildcard would read as working configuration.
  if (!admission.wildcardActive) {
    return (
      <Badge variant="destructive" background>
        Wildcard, inert
      </Badge>
    );
  }

  return (
    <Badge variant="warning" background>
      Wildcard
    </Badge>
  );
}

function AgentCell({
  admission,
}: {
  admission: WorkloadAdmission;
}): JSX.Element {
  // A subject with no assigned agent has no policy and is refused at the token
  // endpoint. This API does not create that state, but an older row can carry it.
  if (admission.agentId.length === 0) {
    return (
      <Badge variant="destructive" background>
        None assigned
      </Badge>
    );
  }

  return <Text>{admission.agentName}</Text>;
}
