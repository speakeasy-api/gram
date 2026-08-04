import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Cell,
  DeleteButton,
  OrDash,
  Row,
  Section,
  Table,
} from "@/components/ema/Section";
import { useUsers } from "@/hooks/use-devidp";
import {
  useCreateEmaAssignment,
  useDeleteEmaAssignment,
  useEmaApps,
  useEmaAssignments,
  useEmaResources,
} from "@/hooks/use-ema";

export const Route = createFileRoute("/ema/assignments")({
  component: AssignmentsPage,
});

function AssignmentsPage() {
  const { data, isLoading } = useEmaAssignments();
  const apps = useEmaApps();
  const resources = useEmaResources();
  const users = useUsers();
  const create = useCreateEmaAssignment();
  const remove = useDeleteEmaAssignment();

  const [appID, setAppID] = useState("");
  const [userID, setUserID] = useState("");
  const [resourceID, setResourceID] = useState("");
  const [scopes, setScopes] = useState("");

  const appLabel = (id: string) =>
    apps.data?.items.find((a) => a.id === id)?.client_id ?? id;
  const userLabel = (id: string) =>
    users.data?.items.find((u) => u.id === id)?.email ?? id;
  const resourceLabel = (id: string) =>
    resources.data?.items.find((r) => r.id === id)?.slug ?? id;

  const submit = () => {
    if (!appID || !userID || !resourceID) return;
    create.mutate(
      {
        app_id: appID,
        user_id: userID,
        resource_id: resourceID,
        granted_scopes: scopes.trim() || undefined,
      },
      { onSuccess: () => setScopes("") },
    );
  };

  return (
    <Section
      title="Assignments"
      description="Which user may drive which app against which resource, and for what scopes. There is no disabled state here — the absence of a row is what denies a mint, so revoking access means deleting one. A mint request is narrowed to the granted scopes; asking only for scopes that are not granted is refused outright."
    >
      <div className="flex flex-wrap items-end gap-3 rounded-lg border border-border p-3">
        <SelectField
          id="assign-app"
          label="App"
          value={appID}
          onChange={setAppID}
          options={(apps.data?.items ?? []).map((a) => ({
            value: a.id,
            label: a.client_id,
          }))}
        />
        <SelectField
          id="assign-user"
          label="User"
          value={userID}
          onChange={setUserID}
          options={(users.data?.items ?? []).map((u) => ({
            value: u.id,
            label: u.email,
          }))}
        />
        <SelectField
          id="assign-resource"
          label="Resource"
          value={resourceID}
          onChange={setResourceID}
          options={(resources.data?.items ?? []).map((r) => ({
            value: r.id,
            label: r.slug,
          }))}
        />
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="assign-scopes">Granted scopes</Label>
          <Input
            id="assign-scopes"
            value={scopes}
            onChange={(e) => setScopes(e.target.value)}
            placeholder="chat.read chat.history"
            className="w-56"
          />
        </div>
        <Button
          onClick={submit}
          disabled={!appID || !userID || !resourceID || create.isPending}
        >
          Assign
        </Button>
      </div>

      {create.error ? (
        <p className="text-sm text-destructive">{String(create.error)}</p>
      ) : null}

      <Table
        headers={["App", "User", "Resource", "Granted scopes", ""]}
        isEmpty={(data?.items ?? []).length === 0}
        empty={isLoading ? "Loading…" : "Nothing assigned — every mint denies."}
      >
        {(data?.items ?? []).map((a) => (
          <Row key={a.id}>
            <Cell mono>{appLabel(a.app_id)}</Cell>
            <Cell>{userLabel(a.user_id)}</Cell>
            <Cell mono>{resourceLabel(a.resource_id)}</Cell>
            <Cell mono>
              <OrDash value={a.granted_scopes} />
            </Cell>
            <Cell className="text-right">
              <DeleteButton
                onClick={() => remove.mutate({ id: a.id })}
                disabled={remove.isPending}
              />
            </Cell>
          </Row>
        ))}
      </Table>
    </Section>
  );
}

function SelectField({
  id,
  label,
  value,
  onChange,
  options,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: Array<{ value: string; label: string }>;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <select
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-9 w-44 rounded-md border border-border bg-background px-2 text-sm"
      >
        <option value="">Select…</option>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </div>
  );
}
