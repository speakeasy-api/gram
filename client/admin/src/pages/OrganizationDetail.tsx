import { useState, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { DataTable as Table, type Column } from "@/components/data-table";
import { useConfirmDialog } from "@/components/ConfirmDialog";
import { ACCOUNT_TYPE_OPTIONS } from "@/lib/accountTypes";
import { cn } from "@/lib/utils";
import {
  organizationMembersQuery,
  organizationProjectsQuery,
  organizationQuery,
  organizationsListQuery,
} from "@/lib/adminQueries";
import {
  errorMessage,
  updateOrganization,
  type AdminOrganization,
  type AdminProject,
  type AdminOrganizationMember,
} from "@/lib/gramAdminApi";

function fmtDate(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "-" : d.toLocaleString();
}

function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid grid-cols-[12rem_1fr] items-baseline gap-3 py-1">
      <span className="text-muted-foreground text-sm">{label}</span>
      <div>{children}</div>
    </div>
  );
}

export function OrganizationDetail(): JSX.Element {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const navigate = useNavigate();

  const { data, isLoading, isError, error } = useQuery({
    ...organizationQuery(idOrSlug),
    enabled: !!idOrSlug,
  });

  return (
    <div className="space-y-6">
      <section>
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h4 className="text-[1.438rem] leading-[1.6] font-light">
              {data ? data.name : "Organization"}
            </h4>
            <span className="text-muted-foreground text-sm">{idOrSlug}</span>
          </div>
          <Button
            variant="ghost"
            size="xs"
            onClick={() => {
              void navigate({ to: "/organizations" });
            }}
          >
            ← Back to list
          </Button>
        </div>

        {isLoading && (
          <span className="text-muted-foreground text-sm">Loading...</span>
        )}
        {isError && (
          <span className="text-muted-foreground text-sm">
            Error: {errorMessage(error)}
          </span>
        )}

        {/* Keyed so an unsaved draft cannot follow the operator to another org. */}
        {data && <OrgDetailsCard key={data.id} org={data} />}
      </section>

      {data && <OrgBottomPanel org={data} />}
    </div>
  );
}

function yesNo(v: boolean): string {
  return v ? "yes" : "no";
}

function OrgDetailsCard({ org }: { org: AdminOrganization }) {
  const qc = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();

  // Only the fields the operator touched live here. The rest read straight
  // from the server record, so a refetch cannot discard an unsaved edit and
  // cannot leave an untouched field showing a stale value.
  const [draft, setDraft] = useState<{
    account_type?: string;
    whitelisted?: boolean;
  }>({});
  const accountType = draft.account_type ?? org.account_type;
  const whitelisted = draft.whitelisted ?? org.whitelisted;

  const mut = useMutation({
    mutationFn: () =>
      updateOrganization({
        id: org.id,
        account_type:
          accountType !== org.account_type ? accountType : undefined,
        whitelisted: whitelisted !== org.whitelisted ? whitelisted : undefined,
      }),
    onSuccess: (updated) => {
      setDraft({});
      qc.setQueryData(organizationQuery(org.id).queryKey, updated);
      qc.setQueryData(organizationQuery(org.slug).queryKey, updated);
      void qc.invalidateQueries({
        queryKey: organizationsListQuery().queryKey,
      });
    },
  });

  const dirty =
    accountType !== org.account_type || whitelisted !== org.whitelisted;

  const handleCancel = () => {
    setDraft({});
  };

  const handleSave = async () => {
    const changes: string[] = [];
    if (accountType !== org.account_type) {
      changes.push(`Account type: ${org.account_type} → ${accountType}`);
    }
    if (whitelisted !== org.whitelisted) {
      changes.push(
        `Whitelisted: ${yesNo(org.whitelisted)} → ${yesNo(whitelisted)}`,
      );
    }

    const confirmed = await confirm({
      title: `Update ${org.name}?`,
      description: changes.join(". ") + ".",
      confirmLabel: "Save",
    });
    if (confirmed) {
      mut.mutate();
    }
  };

  return (
    <div className="border-border bg-muted/10 rounded-md border p-4">
      <Row label="ID">
        <span className="text-sm">{org.id}</span>
      </Row>
      <Row label="Name">
        <span className="text-sm">{org.name}</span>
      </Row>
      <Row label="Slug">
        <span className="text-sm">{org.slug}</span>
      </Row>
      <Row label="Account type">
        <Select
          value={accountType}
          disabled={mut.isPending}
          onValueChange={(v) => setDraft((d) => ({ ...d, account_type: v }))}
        >
          <SelectTrigger className="h-auto w-auto px-2 py-1.5">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {ACCOUNT_TYPE_OPTIONS.map((t) => (
              <SelectItem key={t} value={t}>
                {t}
              </SelectItem>
            ))}
            {!ACCOUNT_TYPE_OPTIONS.includes(org.account_type) && (
              <SelectItem value={org.account_type}>
                {org.account_type}
              </SelectItem>
            )}
          </SelectContent>
        </Select>
      </Row>
      <Row label="Members">
        <span className="text-sm">{org.member_count}</span>
      </Row>
      <Row label="Whitelisted">
        <Switch
          checked={whitelisted}
          disabled={mut.isPending}
          onCheckedChange={(v) => setDraft((d) => ({ ...d, whitelisted: v }))}
        />
      </Row>
      <Row label="WorkOS ID">
        <span
          className={cn("text-sm", !org.workos_id && "text-muted-foreground")}
        >
          {org.workos_id ?? "-"}
        </span>
      </Row>
      <Row label="Disabled at">
        <span
          className={cn("text-sm", !org.disabled_at && "text-muted-foreground")}
        >
          {fmtDate(org.disabled_at)}
        </span>
      </Row>
      <Row label="Free trial started">
        <span className="text-sm">{fmtDate(org.free_trial_started_at)}</span>
      </Row>
      <Row label="Free trial ends">
        <span className="text-sm">{fmtDate(org.free_trial_ends_at)}</span>
      </Row>
      <Row label="Created">
        <span className="text-sm">{fmtDate(org.created_at)}</span>
      </Row>
      <Row label="Updated">
        <span className="text-sm">{fmtDate(org.updated_at)}</span>
      </Row>

      {dirty && (
        <div className="border-border mt-4 flex items-center gap-2 border-t pt-3">
          <Button
            variant="default"
            size="xs"
            disabled={mut.isPending}
            onClick={() => {
              void handleSave();
            }}
          >
            {mut.isPending ? "Saving..." : "Save"}
          </Button>
          <Button
            variant="ghost"
            size="xs"
            disabled={mut.isPending}
            onClick={handleCancel}
          >
            Cancel
          </Button>
          {mut.isError && (
            <span className="text-muted-foreground text-sm">
              Error: {errorMessage(mut.error)}
            </span>
          )}
        </div>
      )}

      {confirmDialog}
    </div>
  );
}

function OrgBottomPanel({ org }: { org: AdminOrganization }) {
  return (
    <section>
      <Tabs defaultValue="projects" className="gap-3">
        <TabsList>
          <TabsTrigger value="projects">Projects</TabsTrigger>
          <TabsTrigger value="members">Members</TabsTrigger>
        </TabsList>

        <TabsContent value="projects">
          <OrgProjectsPanel orgID={org.id} />
        </TabsContent>
        <TabsContent value="members">
          <OrgMembersPanel orgID={org.id} />
        </TabsContent>
      </Tabs>
    </section>
  );
}

function projectsMessage(isLoading: boolean, isError: boolean): string {
  if (isLoading) return "Loading...";
  if (isError) return "Unable to load projects";
  return "No projects in this organization";
}

const PROJECT_COLUMNS: Column<AdminProject>[] = [
  {
    key: "name",
    header: "Name",
    // The link, not the row, carries the keyboard path and the accessible
    // name. It also lets the operator open the project in a new tab.
    render: (p) => (
      <Link
        to="/projects/$idOrSlug"
        params={{ idOrSlug: p.slug || p.id }}
        className="text-sm underline-offset-4 hover:underline focus-visible:underline"
      >
        {p.name}
      </Link>
    ),
  },
  {
    key: "slug",
    header: "Slug",
    render: (p) => <span className="text-sm">{p.slug}</span>,
  },
  {
    key: "id",
    header: "ID",
    render: (p) => (
      <span className="text-muted-foreground text-sm">{p.id}</span>
    ),
  },
  {
    key: "created_at",
    header: "Created",
    render: (p) => <span className="text-sm">{fmtDate(p.created_at)}</span>,
  },
];

function OrgProjectsPanel({ orgID }: { orgID: string }) {
  const navigate = useNavigate();
  const { data, isLoading, isError } = useQuery({
    ...organizationProjectsQuery(orgID),
    enabled: !!orgID,
  });

  const projects = data?.projects ?? [];

  return (
    <div className="max-h-96 overflow-auto rounded-lg border">
      <Table columns={PROJECT_COLUMNS} cellPadding="condensed">
        <Table.Header columns={PROJECT_COLUMNS} />
        <Table.Body>
          {isLoading || projects.length === 0 ? (
            <Table.NoResultsMessage>
              <span className="text-muted-foreground text-sm">
                {projectsMessage(isLoading, isError)}
              </span>
            </Table.NoResultsMessage>
          ) : (
            projects.map((p) => (
              <Table.Row
                key={p.id}
                row={p}
                columns={PROJECT_COLUMNS}
                onClick={() => {
                  void navigate({
                    to: "/projects/$idOrSlug",
                    params: { idOrSlug: p.slug || p.id },
                  });
                }}
              />
            ))
          )}
        </Table.Body>
      </Table>
    </div>
  );
}

const MEMBER_COLUMNS: Column<AdminOrganizationMember>[] = [
  {
    key: "email",
    header: "Email",
    render: (m) => <span className="text-sm">{m.email}</span>,
  },
  {
    key: "display_name",
    header: "Name",
    render: (m) => <span className="text-sm">{m.display_name}</span>,
  },
  {
    key: "id",
    header: "ID",
    render: (m) => (
      <span className="text-muted-foreground text-sm">{m.id}</span>
    ),
  },
  {
    key: "last_login",
    header: "Last login",
    render: (m) => (
      <span className={cn("text-sm", !m.last_login && "text-muted-foreground")}>
        {fmtDate(m.last_login)}
      </span>
    ),
  },
  {
    key: "created_at",
    header: "Joined",
    render: (m) => <span className="text-sm">{fmtDate(m.created_at)}</span>,
  },
];

function membersMessage(isLoading: boolean, isError: boolean): string {
  if (isLoading) return "Loading...";
  if (isError) return "Unable to load members";
  return "No members in this organization";
}

function OrgMembersPanel({ orgID }: { orgID: string }) {
  const { data, isLoading, isError } = useQuery({
    ...organizationMembersQuery(orgID),
    enabled: !!orgID,
  });

  const members = data?.members ?? [];

  return (
    <div className="max-h-96 overflow-auto rounded-lg border">
      <Table columns={MEMBER_COLUMNS} cellPadding="condensed">
        <Table.Header columns={MEMBER_COLUMNS} />
        <Table.Body>
          {isLoading || members.length === 0 ? (
            <Table.NoResultsMessage>
              <span className="text-muted-foreground text-sm">
                {membersMessage(isLoading, isError)}
              </span>
            </Table.NoResultsMessage>
          ) : (
            members.map((m) => (
              <Table.Row key={m.id} row={m} columns={MEMBER_COLUMNS} />
            ))
          )}
        </Table.Body>
      </Table>
    </div>
  );
}
