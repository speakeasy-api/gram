import { useOrganization } from "@/contexts/Auth";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useQueryClient } from "@tanstack/react-query";
import { ApiErrorAlert } from "@/components/api-error-alert";
import { Badge } from "@/components/ui/Badge";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { Text } from "@/components/ui/Text";
import type { OrganizationUser } from "@gram/client/models/components/organizationuser.js";
import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";
import { invalidateAllSlackDirectoryMember } from "@gram/client/react-query/slackDirectoryMember.js";
import { invalidateAllSlackDirectoryMembers } from "@gram/client/react-query/slackDirectoryMembers.js";
import { useSetSlackIdentityMappingMutation } from "@gram/client/react-query/setSlackIdentityMapping.js";
import { SESSION_SECURITY } from "../identity-provider/identityProviderQueries";
import { PersonnelAvatar } from "./MappingStatus";
import { emailsDiffer, mappingFinding } from "./mappingFindings";

const UNMAPPED = "__unmapped";
const normalizedEmail = (email?: string | null) =>
  (email ?? "").trim().toLowerCase();

function personLabel(member: SlackDirectoryMember): string {
  const mapping = member.mapping;
  return mapping ? mapping.displayName || mapping.email : "Not mapped";
}

/**
 * Maps a Slack member to a person as soon as one is picked. The request carries
 * the revision and evidence the row was rendered with, so a stale row is rejected
 * instead of overwriting a newer mapping.
 */
export function SlackPersonnelPicker({
  member,
  people,
  canEdit,
}: {
  member: SlackDirectoryMember;
  people: OrganizationUser[] | undefined;
  canEdit: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const isDemo = useOrganization().slug === DEMO_ORG_SLUG;
  const mapping = member.mapping;
  const finding = mappingFinding(member);
  const refresh = () =>
    Promise.all([
      invalidateAllSlackDirectoryMembers(queryClient),
      invalidateAllSlackDirectoryMember(queryClient),
    ]);
  const mutation = useSetSlackIdentityMappingMutation({
    onSuccess: () => void refresh(),
    // A rejected edit means the row is stale; reload it so the next pick uses current evidence.
    onError: () => void refresh(),
  });

  const email = normalizedEmail(member.email);
  const items: DropdownItem[] =
    member.memberType === "bot"
      ? []
      : (people ?? []).map((person) => {
          const suggested =
            email !== "" &&
            normalizedEmail(person.email) === email &&
            person.userId !== mapping?.userId;
          return {
            value: person.userId,
            label: person.name || person.email,
            description: `${person.email}${suggested ? " · Email match" : ""}`,
            keywords: [person.email],
            icon: (
              <PersonnelAvatar
                name={person.name}
                email={person.email}
                photoUrl={person.photoUrl}
              />
            ),
          };
        });
  if (mapping)
    items.unshift({
      value: UNMAPPED,
      label: "Not mapped",
      description: "Remove the current association",
    });

  let disabledMessage: string | undefined;
  if (!canEdit) disabledMessage = "Only organization admins can map members";
  else if (isDemo) disabledMessage = "The shared demo is read-only";
  else if (!people) disabledMessage = "Loading personnel";
  else if (mutation.isPending) disabledMessage = "Saving mapping";
  else if (member.memberType === "bot" && !mapping)
    disabledMessage = "Bots and apps cannot be assigned to a person";

  const pick = (value: string) => {
    if (disabledMessage || value === (mapping?.userId ?? UNMAPPED)) return;
    mutation.reset();
    mutation.mutate({
      security: SESSION_SECURITY,
      request: {
        setSlackIdentityMappingRequestBody: {
          id: member.id,
          mappingRevision: member.mappingRevision,
          observationToken: member.observationToken,
          userId: value === UNMAPPED ? undefined : value,
        },
      },
    });
  };

  return (
    <div className="min-w-0 space-y-1">
      <Combobox
        id={`slack-personnel-${member.id}`}
        label={`Personnel for ${member.displayName || member.slackUserId}`}
        items={items}
        selected={mapping?.userId}
        onSelectionChange={(item) => pick(item.value)}
        searchable
        searchPlaceholder="Search personnel…"
        variant="tertiary"
        className="h-auto max-w-full justify-start whitespace-normal text-left"
        contentClassName="w-[min(28rem,90vw)]"
        disabledMessage={disabledMessage}
      >
        <span className="flex min-w-0 items-center gap-2">
          {mapping && (
            <PersonnelAvatar
              name={mapping.displayName}
              email={mapping.email}
              photoUrl={mapping.photoUrl}
            />
          )}
          <span className="break-words">
            {mutation.isPending ? "Saving…" : personLabel(member)}
          </span>
        </span>
      </Combobox>
      {finding && (
        <Badge variant="warning" className="whitespace-normal">
          Needs review: {finding}
        </Badge>
      )}
      {mapping && emailsDiffer(member.email, mapping.email) && (
        <Badge variant="warning" className="whitespace-normal">
          Slack email differs from this person’s email
        </Badge>
      )}
      {mutation.isError && (
        <>
          <ApiErrorAlert error={mutation.error} />
          <Text muted small>
            The row was reloaded. Pick again to retry.
          </Text>
        </>
      )}
    </div>
  );
}
