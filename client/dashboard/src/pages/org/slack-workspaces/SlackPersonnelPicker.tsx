import { useOrganization } from "@/contexts/Auth";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useQueryClient } from "@tanstack/react-query";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { TriangleAlert } from "lucide-react";
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

  const warnings = [
    finding && `Needs review: ${finding}`,
    mapping &&
      emailsDiffer(member.email, mapping.email) &&
      `Slack email (${member.email?.trim()}) differs from ${mapping.displayName || mapping.email}’s email (${mapping.email})`,
    mutation.isError &&
      `${mutation.error?.message ?? "The mapping could not be saved"}. The row was reloaded; pick again to retry.`,
  ].filter((warning): warning is string => Boolean(warning));

  return (
    <div className="flex min-w-0 items-center gap-1">
      <Combobox
        id={`slack-personnel-${member.id}`}
        items={items}
        selected={mapping?.userId}
        onSelectionChange={(item) => pick(item.value)}
        searchable
        searchPlaceholder="Search personnel…"
        variant="tertiary"
        className="h-auto min-w-0 flex-1 justify-start py-1 text-left"
        contentClassName="w-[min(28rem,90vw)]"
        disabledMessage={disabledMessage}
      >
        <PersonnelLabel member={member} saving={mutation.isPending} />
      </Combobox>
      {warnings.length > 0 && (
        <SimpleTooltip tooltip={warnings.join(" · ")}>
          <span
            role="img"
            aria-label={warnings.join(". ")}
            className="text-warning inline-flex shrink-0"
          >
            <TriangleAlert className="size-4" />
          </span>
        </SimpleTooltip>
      )}
    </div>
  );
}

function PersonnelLabel({
  member,
  saving,
}: {
  member: SlackDirectoryMember;
  saving: boolean;
}): JSX.Element {
  const mapping = member.mapping;
  if (!mapping)
    return (
      <span className="text-muted-foreground font-normal">
        {saving ? "Saving…" : "Not mapped"}
      </span>
    );
  return (
    <span className="flex min-w-0 items-center gap-3">
      <PersonnelAvatar
        name={mapping.displayName}
        email={mapping.email}
        photoUrl={mapping.photoUrl}
        className="size-8"
      />
      <span className="flex min-w-0 flex-col">
        <span className="truncate font-medium">
          {saving ? "Saving…" : mapping.displayName || mapping.email}
        </span>
        {mapping.displayName && (
          <span className="text-muted-foreground truncate text-xs font-normal">
            {mapping.email}
          </span>
        )}
      </span>
    </span>
  );
}
