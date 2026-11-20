import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useUpdateOrganizationUserSessionIssuerMutation } from "@gram/client/react-query/updateOrganizationUserSessionIssuer.js";
import { useUpdateUserSessionIssuerMutation } from "@gram/client/react-query/updateUserSessionIssuer.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { invalidateIssuers } from "./invalidateIssuers";

/**
 * How a provider's settings can be changed from the page showing them: an
 * organization-wide one through the organization, a project's own from that
 * project, and any other project's not at all.
 */
export type EditPath =
  | { kind: "organization" }
  | { kind: "project" }
  | { kind: "elsewhere"; projectName: string };

export function editPathFor(
  issuer: UserSessionIssuer,
  editableProjectId: string | undefined,
): EditPath {
  if (issuer.projectId === "") return { kind: "organization" };
  if (issuer.projectId === editableProjectId) return { kind: "project" };
  return {
    kind: "elsewhere",
    projectName: issuer.projectName ?? "its project",
  };
}

export const SAVE_SCOPE: Record<"organization" | "project", Scope> = {
  organization: "org:admin",
  project: "project:write",
};

/** The line under a settings panel: who it applies to, or where to change it. */
export function settingsFooterHint(path: EditPath): string {
  if (path.kind === "elsewhere") {
    return `Change this in ${path.projectName}'s MCP Sessions configuration.`;
  }
  if (path.kind === "organization") {
    return "Applies to every MCP server that uses it, in every project.";
  }
  return "Applies to every MCP server that uses it.";
}

export type ProviderSettingsChanges = {
  sessionDurationHours?: number;
  clientIdMetadataAdmissionMode?: "presets" | "open";
};

/**
 * Saves a provider's settings through whichever path can change them, so each
 * section saves on its own.
 */
export function useSaveProviderSettings(
  issuer: UserSessionIssuer,
  path: EditPath,
): {
  save: (changes: ProviderSettingsChanges) => void;
  isPending: boolean;
  error: Error | null;
} {
  const queryClient = useQueryClient();
  const onSuccess = async (): Promise<void> => {
    await invalidateIssuers(queryClient);
    toast.success("Saved");
  };
  const organizationUpdate = useUpdateOrganizationUserSessionIssuerMutation({
    onSuccess,
  });
  const projectUpdate = useUpdateUserSessionIssuerMutation({ onSuccess });
  const update =
    path.kind === "organization" ? organizationUpdate : projectUpdate;

  return {
    save: (changes) => {
      const form = { id: issuer.id, ...changes };
      if (path.kind === "organization") {
        organizationUpdate.mutate({
          request: { updateOrganizationUserSessionIssuerForm: form },
        });
      } else if (path.kind === "project") {
        projectUpdate.mutate({
          request: { updateUserSessionIssuerForm: form },
        });
      }
    },
    isPending: update.isPending,
    error: update.error,
  };
}
