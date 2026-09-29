import { useId, useState } from "react";
import { Button } from "@/components/ui/Button";

import { Checkbox } from "@/components/ui/Checkbox";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/Select";
import type { RoleProvisioningStatus } from "@gram/client/models/components/roleprovisioningstatus.js";
import type { ConfigureRoleProvisioningRequestBody } from "@gram/client/models/components/configureroleprovisioningrequestbody.js";

export type RolePluginStatus = RoleProvisioningStatus;
export type RolePluginConfiguration = ConfigureRoleProvisioningRequestBody;
const NO_PROJECT = "00000000-0000-0000-0000-000000000000";
const label = (value: string) => value.replaceAll("_", " ");

/** Shared confirmation editor: status is authoritative; edits never claim provisioning. */
export function RolePluginSettings({
  status: loadedStatus,
  preferredProjectId,
  saving,
  error,
  onSave,
  onReload,
}: {
  status: RolePluginStatus;
  preferredProjectId?: string;
  saving: boolean;
  error?: string;
  onSave: (configuration: RolePluginConfiguration) => void;
  onReload: () => void;
}): JSX.Element {
  // Keep the version paired with this draft; background reads must not silently
  // replace its conflict token. Explicit save/reload remounts the editor.
  const [status] = useState(loadedStatus);
  const heading = useId();
  const initial = status.version === 0;
  const proposed =
    status.projectId ||
    (initial && status.projects.find((p) => p.id === preferredProjectId)?.id) ||
    (initial ? status.projects[0]?.id : undefined) ||
    "";
  const [enabled, setEnabled] = useState(status.enabled);
  const [projectId, setProjectId] = useState(proposed);
  const [roles, setRoles] = useState(() =>
    status.roles.map((role) => ({
      roleUrn: role.roleUrn,
      enabled: (initial && !role.configured) || role.enabled,
      projectId:
        (initial && !role.configured ? proposed : role.projectId) || "",
    })),
  );
  const projectName = (id: string | null | undefined) =>
    status.projects.find((p) => p.id === id)?.name ?? id ?? "None";
  function projectPicker(
    value: string,
    name: string,
    change: (value: string) => void,
  ) {
    return (
      <Select
        value={value || NO_PROJECT}
        onValueChange={(id) => change(id === NO_PROJECT ? "" : id)}
        disabled={saving}
      >
        <SelectTrigger aria-label={name}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={NO_PROJECT}>Choose a project</SelectItem>
          {status.projects.map((project) => (
            <SelectItem key={project.id} value={project.id}>
              {project.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    );
  }
  return (
    <section
      aria-labelledby={heading}
      className="space-y-4 rounded-lg border p-4"
    >
      <h3 id={heading} className="font-semibold">
        Automatic role plugins
      </h3>
      <p className="text-muted-foreground text-sm">
        Create empty plugins for selected IdP roles. Add servers in plugin
        management; existing server permissions never populate plugin contents.
        Publication is separate.
      </p>
      <p className="text-sm">
        Saved organization setting:{" "}
        <strong>{status.enabled ? "Enabled" : "Off"}</strong>
      </p>
      <fieldset disabled={saving} className="space-y-4">
        <label className="flex items-center gap-2 text-sm">
          <Checkbox
            disabled={saving}
            checked={enabled}
            onCheckedChange={(checked) => setEnabled(checked === true)}
          />
          Enable automatic role plugins
        </label>
        <p className="text-muted-foreground text-sm">
          Turning this off stops automation and preserves plugins, contents,
          audiences, grants, exclusions, and destinations. Directory sync
          continues independently.
        </p>
        <label className="flex flex-col gap-2 text-sm">
          Default destination for new roles
          {projectPicker(projectId, "Default destination project", (id) => {
            setProjectId(id);
            setRoles((previous) =>
              previous.map((role) =>
                role.projectId ? role : { ...role, projectId: id },
              ),
            );
          })}
        </label>
        {status.projects.length === 0 && (
          <p role="status" className="text-sm">
            Choose a project when one is available. No project will be created;
            directory sync is unaffected.
          </p>
        )}
        <p className="text-muted-foreground text-sm">
          Review each role before saving. Unchecked roles remain excluded.
          Existing destinations stay unchanged when the default changes.
        </p>
        {status.roles.length === 0 && (
          <p className="text-sm">
            No IdP roles yet. Future roles follow the saved organization
            setting.
          </p>
        )}
        {status.roles.map((role, index) => {
          const draft = roles[index]!;
          return (
            <article
              key={role.roleUrn}
              className="space-y-2 rounded-md border p-3"
            >
              <label className="flex items-center gap-2 font-medium">
                <Checkbox
                  disabled={saving}
                  checked={draft.enabled}
                  onCheckedChange={(checked) =>
                    setRoles((previous) =>
                      previous.map((item) =>
                        item.roleUrn === role.roleUrn
                          ? { ...item, enabled: checked === true }
                          : item,
                      ),
                    )
                  }
                />
                {role.name}
              </label>
              <p className="text-muted-foreground text-sm">
                Saved role setting:{" "}
                {role.configured === false || (initial && !role.configured)
                  ? "Not configured"
                  : role.enabled
                    ? "Enabled"
                    : "Excluded"}
              </p>
              {projectPicker(
                draft.projectId,
                `Destination for ${role.name}`,
                (id) =>
                  setRoles((previous) =>
                    previous.map((item) =>
                      item.roleUrn === role.roleUrn
                        ? { ...item, projectId: id }
                        : item,
                    ),
                  ),
              )}
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                <dt>Saved desired destination</dt>
                <dd>{projectName(role.projectId)}</dd>
                <dt>Applied destination</dt>
                <dd>{projectName(role.appliedProjectId)}</dd>
                <dt>Associated plugin</dt>
                <dd className="break-all">
                  {role.pluginId ?? "Not provisioned"}
                </dd>
                <dt>Origin audience</dt>
                <dd>{label(role.originAudience)}</dd>
                <dt>Publication</dt>
                <dd>{label(role.publicationStatus)}</dd>
              </dl>
              {role.pendingReason && (
                <p role="status" className="text-sm">
                  Pending: {label(role.pendingReason)}.
                  {role.pendingReason === "audience_approval_required" &&
                    " Review the existing audience approval request before restoration can proceed."}
                </p>
              )}
            </article>
          );
        })}
        <p className="text-muted-foreground text-sm">
          Saving records intent only. Enabled does not mean queued, provisioned,
          or published.
        </p>
        {error && (
          <div role="alert" className="space-y-2 text-sm">
            <p>{error}</p>
          </div>
        )}
        <Button variant="secondary" onClick={onReload}>
          Reload saved settings
        </Button>
        <Button
          onClick={() =>
            onSave(
              !enabled && status.enabled
                ? { expectedVersion: status.version, enabled: false }
                : {
                    expectedVersion: status.version,
                    enabled,
                    projectId: projectId || NO_PROJECT,
                    roles: roles.map((role) => ({
                      ...role,
                      projectId: role.projectId || NO_PROJECT,
                    })),
                  },
            )
          }
        >
          {saving ? "Saving…" : "Save role plugin settings"}
        </Button>
      </fieldset>
    </section>
  );
}
