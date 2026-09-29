import { useId, useState } from "react";
import { Button } from "@/components/ui/Button";

import { Info } from "lucide-react";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/Popover";
import { Switch } from "@/components/ui/Switch";
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
        <SelectTrigger aria-label={name} className="w-full min-w-0">
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
        Create an empty plugin for each selected IdP role.
      </p>
      <fieldset disabled={saving} className="space-y-4">
        <div className="flex items-center gap-2">
          <label className="flex items-center gap-3 text-sm font-medium">
            <Switch
              disabled={saving}
              checked={enabled}
              onCheckedChange={setEnabled}
            />
            Enable automatic role plugins
          </label>
          <Popover>
            <PopoverTrigger asChild>
              <button
                type="button"
                aria-label="About automatic role plugins"
                className="text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex size-6 items-center justify-center rounded-sm focus-visible:outline-none focus-visible:ring-2"
              >
                <Info className="size-4" aria-hidden="true" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="start" className="text-sm">
              Creates an empty plugin for each selected IdP role in its chosen
              project. You add tools and publish the plugins separately.
            </PopoverContent>
          </Popover>
        </div>
        <label className="flex flex-col gap-2 text-sm">
          Default project
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
            No projects available. Create a project to choose a destination.
          </p>
        )}
        {status.roles.length === 0 && (
          <p className="text-muted-foreground text-sm">
            No IdP roles synced yet.
          </p>
        )}
        <div className="divide-y">
          {status.roles.map((role, index) => {
            const draft = roles[index]!;
            return (
              <div
                key={role.roleUrn}
                className="grid gap-3 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,1fr)] sm:items-center"
              >
                <label className="flex min-w-0 items-center gap-2 text-sm font-medium">
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
                  <span className="break-words">{role.name}</span>
                </label>
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
                {role.pendingReason === "audience_approval_required" && (
                  <p
                    role="status"
                    className="text-muted-foreground text-sm sm:col-span-2"
                  >
                    Review the pending audience approval request to restore
                    access.
                  </p>
                )}
              </div>
            );
          })}
        </div>
        {error && (
          <div className="space-y-2 text-sm">
            <p role="alert">{error}</p>
            <Button variant="secondary" onClick={onReload}>
              Reload saved settings
            </Button>
          </div>
        )}
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
          {saving ? "Saving…" : "Save changes"}
        </Button>
      </fieldset>
    </section>
  );
}
