import { useState, useEffect, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams, useNavigate } from "react-router";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { DataTable as Table, type Column } from "@/components/data-table";
import { cn } from "@/lib/utils";
import {
  getOrganization,
  listOrganizationProjects,
  listOrganizationMembers,
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
  const { idOrSlug = "" } = useParams<{ idOrSlug: string }>();
  const navigate = useNavigate();

  const { data, isLoading, isError, error } = useQuery<AdminOrganization>({
    queryKey: ["gram-admin-organization", idOrSlug],
    queryFn: () => getOrganization(idOrSlug),
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
              void navigate("/organizations");
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
            Error: {(error as Error).message}
          </span>
        )}

        {data && <OrgDetailsCard org={data} />}
      </section>

      {data && <OrgBottomPanel org={data} />}
    </div>
  );
}

const accountTypeOptions = ["free", "pro", "enterprise"];

function OrgDetailsCard({ org }: { org: AdminOrganization }) {
  const qc = useQueryClient();
  const [accountType, setAccountType] = useState(org.account_type);
  const [whitelisted, setWhitelisted] = useState(org.whitelisted);

  useEffect(() => {
    setAccountType(org.account_type);
    setWhitelisted(org.whitelisted);
  }, [org.account_type, org.whitelisted]);

  const mut = useMutation({
    mutationFn: () =>
      updateOrganization({
        id: org.id,
        account_type:
          accountType !== org.account_type ? accountType : undefined,
        whitelisted: whitelisted !== org.whitelisted ? whitelisted : undefined,
      }),
    onSuccess: (updated) => {
      qc.setQueryData(["gram-admin-organization", org.id], updated);
      qc.setQueryData(["gram-admin-organization", org.slug], updated);
      void qc.invalidateQueries({ queryKey: ["gram-admin-organizations"] });
    },
  });

  const dirty =
    accountType !== org.account_type || whitelisted !== org.whitelisted;

  const handleCancel = () => {
    setAccountType(org.account_type);
    setWhitelisted(org.whitelisted);
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
        <Select value={accountType} onValueChange={setAccountType}>
          <SelectTrigger className="h-auto w-auto px-2 py-1.5">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {accountTypeOptions.map((t) => (
              <SelectItem key={t} value={t}>
                {t}
              </SelectItem>
            ))}
            {!accountTypeOptions.includes(org.account_type) && (
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
        <Switch checked={whitelisted} onCheckedChange={setWhitelisted} />
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
            onClick={() => mut.mutate()}
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
              Error: {(mut.error as Error).message}
            </span>
          )}
        </div>
      )}
    </div>
  );
}

function OrgBottomPanel({ org }: { org: AdminOrganization }) {
  const [activeTab, setActiveTab] = useState<"projects" | "members">(
    "projects",
  );

  return (
    <section className="space-y-3">
      <Tabs
        value={activeTab}
        onValueChange={(tab) => setActiveTab(tab as "projects" | "members")}
      >
        <TabsList>
          <TabsTrigger value="projects">Projects</TabsTrigger>
          <TabsTrigger value="members">Members</TabsTrigger>
        </TabsList>
      </Tabs>

      {activeTab === "projects" && <OrgProjectsPanel orgID={org.id} />}
      {activeTab === "members" && <OrgMembersPanel orgID={org.id} />}
    </section>
  );
}

function OrgProjectsPanel({ orgID }: { orgID: string }) {
  const navigate = useNavigate();
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["gram-admin-organization-projects", orgID],
    queryFn: () => listOrganizationProjects(orgID),
    enabled: !!orgID,
  });

  const columns: Column<AdminProject>[] = [
    {
      key: "name",
      header: "Name",
      render: (p) => <span className="text-sm">{p.name}</span>,
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

  const projects = data?.projects ?? [];

  return (
    <div className="max-h-96 overflow-auto rounded-lg border">
      <Table columns={columns} cellPadding="condensed">
        <Table.Header columns={columns} />
        <Table.Body>
          {isLoading || projects.length === 0 ? (
            <Table.NoResultsMessage>
              <span className="text-muted-foreground text-sm">
                {isLoading
                  ? "Loading..."
                  : isError
                    ? `Error: ${(error as Error)?.message ?? "unknown"}`
                    : "No projects in this organization"}
              </span>
            </Table.NoResultsMessage>
          ) : (
            projects.map((p) => (
              <Table.Row
                key={p.id}
                row={p}
                columns={columns}
                onClick={() => {
                  void navigate(
                    `/projects/${encodeURIComponent(p.slug || p.id)}`,
                  );
                }}
              />
            ))
          )}
        </Table.Body>
      </Table>
    </div>
  );
}

function OrgMembersPanel({ orgID }: { orgID: string }) {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["gram-admin-organization-members", orgID],
    queryFn: () => listOrganizationMembers(orgID),
    enabled: !!orgID,
  });

  const columns: Column<AdminOrganizationMember>[] = [
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
        <span
          className={cn("text-sm", !m.last_login && "text-muted-foreground")}
        >
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

  const members = data?.members ?? [];

  return (
    <div className="max-h-96 overflow-auto rounded-lg border">
      <Table columns={columns} cellPadding="condensed">
        <Table.Header columns={columns} />
        <Table.Body>
          {isLoading || members.length === 0 ? (
            <Table.NoResultsMessage>
              <span className="text-muted-foreground text-sm">
                {isLoading
                  ? "Loading..."
                  : isError
                    ? `Error: ${(error as Error)?.message ?? "unknown"}`
                    : "No members in this organization"}
              </span>
            </Table.NoResultsMessage>
          ) : (
            members.map((m) => (
              <Table.Row key={m.id} row={m} columns={columns} />
            ))
          )}
        </Table.Body>
      </Table>
    </div>
  );
}
