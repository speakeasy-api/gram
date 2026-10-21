import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { GoogleChatPicker } from "./GoogleChatPicker";
import { ApiErrorAlert } from "@/components/api-error-alert";
import {
  useGoogleChatMembers,
  invalidateAllGoogleChatMembers,
} from "@gram/client/react-query/googleChatMembers.js";
import { useSyncGoogleChatMembersMutation } from "@gram/client/react-query/syncGoogleChatMembers.js";
import { useMapGoogleChatMemberMutation } from "@gram/client/react-query/mapGoogleChatMember.js";
import { useUpdateGoogleChatBindingMutation } from "@gram/client/react-query/updateGoogleChatBinding.js";
import { useListOrganizationUsers } from "@gram/client/react-query/listOrganizationUsers.js";
import type { GoogleChatBinding } from "@gram/client/models/components/googlechatbinding.js";
import { SESSION_SECURITY } from "../identity-provider/identityProviderQueries";
import { useOrganization } from "@/contexts/Auth";
import { DEMO_ORG_SLUG } from "@/lib/demo";

export function GoogleChatBindingPanel({
  binding,
  onChanged,
  canConnect,
}: {
  binding: GoogleChatBinding;
  canConnect: boolean;
  onChanged: () => Promise<unknown>;
}): JSX.Element {
  const cache = useQueryClient();
  const org = useOrganization();
  const [expanded, setExpanded] = useState(false);
  const members = useGoogleChatMembers(
    { bindingId: binding.id },
    SESSION_SECURITY,
    { enabled: expanded, retry: false, throwOnError: false },
  );
  const people = useListOrganizationUsers(undefined, SESSION_SECURITY, {
    enabled: expanded,
    retry: false,
    throwOnError: false,
  });
  const refresh = async () => {
    await invalidateAllGoogleChatMembers(cache);
    await onChanged();
  };
  const sync = useSyncGoogleChatMembersMutation({ onSuccess: refresh });
  const map = useMapGoogleChatMemberMutation({
    onSuccess: refresh,
    onError: refresh,
  });
  const update = useUpdateGoogleChatBindingMutation({
    onSuccess: refresh,
    onError: refresh,
  });
  const disabled =
    org.slug === DEMO_ORG_SLUG ||
    map.isPending ||
    sync.isPending ||
    update.isPending;
  const action = (value: "pause" | "resume" | "repair") => {
    update.mutate({
      request: {
        updateBindingGoogleChatRequestBody: {
          bindingId: binding.id,
          revision: binding.revision,
          action: value,
        },
      },
      security: SESSION_SECURITY,
    });
  };
  return (
    <article className="border border-border p-5 space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Heading variant="h4">{binding.displayName}</Heading>
          <Text muted small>
            {binding.spaceName} · Command: {binding.prefix}
          </Text>
        </div>
        <Badge variant={binding.status === "active" ? "success" : "warning"}>
          {binding.status}
        </Badge>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button variant="secondary" onClick={() => setExpanded(!expanded)}>
          <Button.Text>
            {expanded ? "Hide participants" : "Manage participants"}
          </Button.Text>
        </Button>
        <Button
          variant="secondary"
          disabled={disabled || (binding.status !== "active" && !canConnect)}
          onClick={() =>
            action(binding.status === "active" ? "pause" : "resume")
          }
        >
          <Button.Text>
            {binding.status === "active" ? "Pause" : "Resume"}
          </Button.Text>
        </Button>
        {binding.status === "error" && (
          <Button
            disabled={disabled || !canConnect}
            onClick={() => action("repair")}
          >
            <Button.Text>Retry setup</Button.Text>
          </Button>
        )}
      </div>
      {binding.lastError && (
        <Text>
          {binding.lastError === "google_approval_required"
            ? "Ask your Google Workspace administrator to approve the Speakeasy Chat app and its message access, then retry setup."
            : "Setup needs attention. Check Google access, then retry setup."}
        </Text>
      )}
      <ApiErrorAlert
        error={
          members.error ??
          people.error ??
          map.error ??
          sync.error ??
          update.error
        }
      />
      {expanded && (
        <div className="space-y-3">
          <Text muted small>
            Only active, mapped people can invoke this assistant. Their existing
            permissions still apply. Refresh after membership changes.
          </Text>
          <Button
            variant="secondary"
            disabled={disabled || !canConnect}
            onClick={() => {
              sync.mutate({
                request: {
                  syncMembersGoogleChatRequestBody: { bindingId: binding.id },
                },
                security: SESSION_SECURITY,
              });
            }}
          >
            <Button.Text>Refresh participants</Button.Text>
          </Button>
          {members.data?.members.map((member) => (
            <div
              className="grid gap-3 border-t border-border pt-3 md:grid-cols-2"
              key={member.id}
            >
              <div>
                <Text>{member.displayName || member.googleUserName}</Text>
                <Text muted small>
                  {member.googleUserName}
                  {!member.active ? " · No longer in space" : ""}
                </Text>
              </div>
              <GoogleChatPicker
                id={`google-member-${member.id}`}
                label="Speakeasy identity"
                selected={member.userId ?? "unmapped"}
                disabledMessage={
                  disabled || !member.active ? "Unavailable" : undefined
                }
                items={[
                  { value: "unmapped", label: "Not mapped — cannot invoke" },
                  ...(people.data?.users.map((p) => ({
                    value: p.userId,
                    label: p.name || p.email,
                    description: p.email,
                  })) ?? []),
                ]}
                onSelectionChange={(item) => {
                  const value = item.value;
                  map.mutate({
                    request: {
                      mapMemberGoogleChatRequestBody: {
                        bindingId: binding.id,
                        memberId: member.id,
                        revision: member.revision,
                        userId: value === "unmapped" ? undefined : value,
                      },
                    },
                    security: SESSION_SECURITY,
                  });
                }}
              />
            </div>
          ))}
          {members.data?.members.length === 0 && (
            <Text muted>
              No participants found. Refresh after invitations are accepted.
            </Text>
          )}
        </div>
      )}
    </article>
  );
}
