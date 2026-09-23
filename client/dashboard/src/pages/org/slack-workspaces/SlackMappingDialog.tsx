import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiErrorAlert } from "@/components/api-error-alert";
import { Button } from "@/components/ui/Button";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import type { SlackDirectoryMember } from "@gram/client/models/components/slackdirectorymember.js";
import type { OrganizationUser } from "@gram/client/models/components/organizationuser.js";
import {
  useSlackDirectoryMember,
  invalidateAllSlackDirectoryMember,
} from "@gram/client/react-query/slackDirectoryMember.js";
import { invalidateAllSlackDirectoryMembers } from "@gram/client/react-query/slackDirectoryMembers.js";
import { useListOrganizationUsers } from "@gram/client/react-query/listOrganizationUsers.js";
import { useSetSlackIdentityMappingMutation } from "@gram/client/react-query/setSlackIdentityMapping.js";
import {
  SESSION_SECURITY,
  inlineError,
} from "../identity-provider/identityProviderQueries";
import { MappingStatus, PersonnelAvatar } from "./MappingStatus";
import { stateLabels, typeLabels } from "./memberLabels";

const UNMAPPED = "__unmapped";
const normalizedEmail = (email: string) => email.trim().toLowerCase();

export function SlackMappingDialog({
  id,
  onClose,
}: {
  id: string;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [pending, setPending] = useState(false);
  const member = useSlackDirectoryMember({ id }, SESSION_SECURITY, {
    retry: false,
    throwOnError: false,
    staleTime: 0,
    refetchOnMount: "always",
    refetchOnWindowFocus: false,
  });
  const people = useListOrganizationUsers(undefined, SESSION_SECURITY, {
    retry: false,
    throwOnError: false,
    staleTime: 0,
    refetchOnMount: "always",
    refetchOnWindowFocus: false,
  });
  const ready =
    member.data &&
    people.data &&
    !member.isFetching &&
    !people.isFetching &&
    !member.isError &&
    !people.isError;
  const close = () => {
    if (!pending) onClose();
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <Dialog.Content
        closeable={!pending}
        className="max-h-[90dvh] overflow-y-auto"
      >
        <Dialog.Header>
          <Dialog.Title>Map Slack identity</Dialog.Title>
          <Dialog.Description>
            Select an existing person in this organization and confirm the
            association.
          </Dialog.Description>
        </Dialog.Header>
        <ApiErrorAlert error={member.error ?? people.error} />
        {(member.isError || people.isError) && (
          <Button
            variant="secondary"
            onClick={() => {
              void member.refetch();
              void people.refetch();
            }}
          >
            <Button.Text>Try loading again</Button.Text>
          </Button>
        )}
        {(member.isFetching || people.isFetching) && (
          <Text muted role="status">
            Loading current member and personnel…
          </Text>
        )}
        {ready && (
          <MappingForm
            key={`${member.data.mappingRevision}:${member.data.observationToken}`}
            member={member.data}
            people={people.data.users}
            onPending={setPending}
            onClose={close}
            onReload={() => {
              void member.refetch();
              void people.refetch();
            }}
            onSaved={async () => {
              await Promise.all([
                invalidateAllSlackDirectoryMembers(queryClient),
                invalidateAllSlackDirectoryMember(queryClient),
              ]);
              onClose();
            }}
          />
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function MappingForm({
  member,
  people,
  onPending,
  onClose,
  onSaved,
  onReload,
}: {
  member: SlackDirectoryMember;
  people: OrganizationUser[];
  onPending: (value: boolean) => void;
  onClose: () => void;
  onSaved: () => Promise<void>;
  onReload: () => void;
}): JSX.Element {
  const [selected, setSelected] = useState<string | undefined>(
    member.mapping?.userId,
  );
  const mutation = useSetSlackIdentityMappingMutation({ onError: inlineError });
  const email = normalizedEmail(member.email ?? "");
  const items: DropdownItem[] = people.map((person) => {
    const suggested =
      email !== "" &&
      normalizedEmail(person.email) === email &&
      person.userId !== member.mapping?.userId;
    return {
      value: person.userId,
      label: person.name || person.email,
      description: `${person.email}${suggested ? " · Email match suggestion" : ""}`,
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
  if (member.memberType === "bot") items.length = 0;
  if (member.mapping)
    items.unshift({
      value: UNMAPPED,
      label: "Not mapped",
      description: "Remove the current association",
    });
  const selectedPerson = people.find((person) => person.userId === selected);
  let selectionLabel = "Select a person…";
  if (selectedPerson)
    selectionLabel = selectedPerson.name || selectedPerson.email;
  if (selected === UNMAPPED) selectionLabel = "Not mapped";
  const validSelection =
    selected === UNMAPPED ||
    (Boolean(selectedPerson) && member.memberType !== "bot");
  const save = async () => {
    onPending(true);
    try {
      await mutation.mutateAsync({
        security: SESSION_SECURITY,
        request: {
          setSlackIdentityMappingRequestBody: {
            id: member.id,
            mappingRevision: member.mappingRevision,
            observationToken: member.observationToken,
            userId: selected === UNMAPPED ? undefined : selected,
          },
        },
      });
      await onSaved();
    } catch {
      /* The mutation error is shown inline. */
    } finally {
      onPending(false);
    }
  };
  return (
    <>
      <div className="space-y-2 border p-4">
        <Text className="font-medium">
          {member.displayName || member.slackUserId}
        </Text>
        <Text muted small>
          {member.email || "Email not provided"} ·{" "}
          {member.workspaceName || member.workspaceId}
        </Text>
        <Text small>
          Directory state: {stateLabels[member.status]} ·{" "}
          {typeLabels[member.memberType]}
        </Text>
        {!member.observedInLastSync && (
          <Text muted small>
            Not seen in the last full sync
          </Text>
        )}
        <MappingStatus member={member} />
      </div>
      <div className="space-y-2">
        <label htmlFor="slack-personnel" className="text-sm font-medium">
          Personnel
        </label>
        <Combobox
          id="slack-personnel"
          items={items}
          selected={selected}
          onSelectionChange={(item) => setSelected(item.value)}
          searchable
          searchPlaceholder="Search personnel…"
          className="w-full"
          contentClassName="w-[min(28rem,90vw)]"
          disabledMessage={mutation.isPending ? "Saving mapping" : undefined}
        >
          {selectionLabel}
        </Combobox>
        <Text muted small>
          Email matches are suggestions. Confirm only after checking the person.
        </Text>
        {member.memberType === "bot" && (
          <Text small>
            Bots and apps cannot be assigned to a person. You can remove an
            existing mapping.
          </Text>
        )}
        <Text muted small>
          This association grants no permissions. Directory state and connection
          freshness still apply.
        </Text>
      </div>
      <ApiErrorAlert error={mutation.error} />
      {mutation.isError && (
        <Button variant="secondary" onClick={onReload}>
          <Button.Text>Reload member and review</Button.Text>
        </Button>
      )}
      <Dialog.Footer>
        <Button
          variant="secondary"
          disabled={mutation.isPending}
          onClick={onClose}
        >
          <Button.Text>Cancel</Button.Text>
        </Button>
        <Button
          disabled={!validSelection || mutation.isPending || mutation.isError}
          onClick={() => {
            void save();
          }}
        >
          <Button.Text>
            {mutation.isPending ? "Saving…" : "Confirm"}
          </Button.Text>
        </Button>
      </Dialog.Footer>
    </>
  );
}
