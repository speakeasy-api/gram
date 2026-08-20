import { MultiSelect } from "@/components/ui/MultiSelect";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import type { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";
import type {
  PluginAudience,
  PluginAudienceKind,
} from "@gram/client/models/components/pluginaudience.js";
import { useAudiences } from "@gram/client/react-query/audiences";
import { useSetPluginAssignmentsMutation } from "@gram/client/react-query/setPluginAssignments";
import { Button } from "@/components/ui/Button";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  audienceKindForPrincipal,
  audienceMapByUrn,
  describePrincipal,
  memberCountDescription,
  principalIcon,
} from "./principals";

const audienceGroups: {
  value: PluginAudienceKind;
  heading: string;
  icon: ReturnType<typeof principalIcon>;
}[] = [
  { value: "everyone", heading: "Everyone", icon: principalIcon("everyone") },
  { value: "role", heading: "Roles", icon: principalIcon("role") },
  {
    value: "directory_group",
    heading: "Directory groups",
    icon: principalIcon("directory_group"),
  },
  {
    value: "directory_attribute",
    heading: "Directory attributes",
    icon: principalIcon("directory_attribute"),
  },
];

function existingAssignmentOption(
  urn: string,
  audienceByUrn: Map<string, PluginAudience>,
  description: string,
) {
  const principal = describePrincipal(urn, new Map(), new Map(), audienceByUrn);
  return {
    label: principal.label,
    value: urn,
    description,
    disabled: true,
  };
}

function availableAudienceOptions(
  audienceType: PluginAudienceKind,
  audiences: PluginAudience[],
  disabled: boolean,
) {
  return audiences
    .filter((audience) => audience.kind === audienceType)
    .map((audience) => ({
      label: audience.displayName,
      value: audience.principalUrn,
      description: memberCountDescription(audience.memberCount),
      disabled,
    }));
}

function unavailableAudienceOptions(
  audienceType: PluginAudienceKind,
  selected: string[],
  audienceByUrn: Map<string, PluginAudience>,
) {
  return selected
    .filter(
      (urn) =>
        audienceKindForPrincipal(urn, audienceByUrn) === audienceType &&
        !audienceByUrn.has(urn),
    )
    .map((urn) =>
      existingAssignmentOption(
        urn,
        audienceByUrn,
        "Unavailable. Remove this assignment to stop using it.",
      ),
    );
}

export function PluginAssignmentsSheet({
  pluginId,
  pluginName,
  assignments,
  open,
  onOpenChange,
  onSaved,
}: {
  pluginId: string;
  pluginName: string;
  assignments: PluginAssignment[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}): JSX.Element {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col gap-0 sm:max-w-md"
      >
        <SheetHeader className="px-6 pt-6">
          <SheetTitle>Manage assignments</SheetTitle>
          <SheetDescription>
            Choose who receives <strong>{pluginName}</strong>. Assignments apply
            on each device's next sync.
          </SheetDescription>
        </SheetHeader>
        {/* Remount the editor on each open so its draft always re-seeds from the
            plugin's current assignments. */}
        {open && (
          <AssignmentsEditor
            pluginId={pluginId}
            assignments={assignments}
            onOpenChange={onOpenChange}
            onSaved={onSaved}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function AssignmentsEditor({
  pluginId,
  assignments,
  onOpenChange,
  onSaved,
}: {
  pluginId: string;
  assignments: PluginAssignment[];
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}): JSX.Element {
  const audiencesQuery = useAudiences();
  const { data: audiencesData } = audiencesQuery;
  const audiences = useMemo(
    () => audiencesData?.audiences ?? [],
    [audiencesData?.audiences],
  );

  const initialUrns = useMemo(
    () => assignments.map((a) => a.principalUrn),
    [assignments],
  );
  const [selected, setSelected] = useState<string[]>(initialUrns);

  const audienceByUrn = useMemo(() => audienceMapByUrn(audiences), [audiences]);
  const canSelectAudiences =
    !audiencesQuery.isPending && !audiencesQuery.isError;
  const availableAudienceGroups = useMemo(
    () =>
      audienceGroups.map((group) => ({
        heading: group.heading,
        icon: group.icon,
        options: availableAudienceOptions(
          group.value,
          audiences,
          !canSelectAudiences,
        ),
      })),
    [audiences, canSelectAudiences],
  );
  const unavailableAudienceGroups = useMemo(
    () =>
      audienceGroups.map((group) => ({
        heading: group.heading,
        icon: group.icon,
        options: unavailableAudienceOptions(
          group.value,
          selected,
          audienceByUrn,
        ),
      })),
    [audienceByUrn, selected],
  );
  const options = useMemo(
    () =>
      availableAudienceGroups
        .map((group, index) => ({
          heading: group.heading,
          icon: group.icon,
          options: [
            ...group.options,
            ...unavailableAudienceGroups[index]!.options,
          ],
        }))
        .filter((group) => group.options.length > 0),
    [availableAudienceGroups, unavailableAudienceGroups],
  );
  const legacyOptions = useMemo(
    () =>
      selected
        .filter((urn) => !audienceKindForPrincipal(urn, audienceByUrn))
        .map((urn) =>
          existingAssignmentOption(
            urn,
            audienceByUrn,
            "Legacy assignment. Remove it to stop using this audience.",
          ),
        ),
    [audienceByUrn, selected],
  );

  const groupedOptions = useMemo(
    () =>
      legacyOptions.length > 0
        ? [
            ...options,
            {
              heading: "Legacy assignments",
              options: legacyOptions,
            },
          ]
        : options,
    [legacyOptions, options],
  );

  const mutation = useSetPluginAssignmentsMutation({
    onSuccess: () => {
      onSaved();
      onOpenChange(false);
    },
    onError: () => {
      toast.error("Failed to update assignments");
    },
  });

  const handleSave = () => {
    mutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        setPluginAssignmentsForm: {
          pluginId,
          principalUrns: Array.from(new Set(selected)),
        },
      },
    });
  };

  return (
    <>
      <div className="flex-1 overflow-y-auto px-6 py-4">
        <label className="mb-2 block text-sm font-medium">
          Assigned audiences
        </label>
        <MultiSelect
          options={groupedOptions}
          defaultValue={selected}
          onValueChange={setSelected}
          placeholder="Select audiences"
          badgeClassName="h-6 gap-1.5 px-1.5 font-sans text-xs normal-case tracking-normal"
          searchable
          hideSelectAll
          modalPopover
          maxCount={20}
          popoverClassName="w-[var(--radix-popover-trigger-width)] min-w-0 max-w-none [&_[cmdk-group-heading]]:px-0 [&_[cmdk-group-heading]]:py-1 [&_[cmdk-input-wrapper]]:px-0 [&_[cmdk-item]]:py-1 [&_[cmdk-item]]:pl-2 [&_[cmdk-item]]:pr-0 [&_[data-slot=command-group]]:p-0 [&_[data-slot=command-group]+[data-slot=command-group]]:pt-2 [&_[data-slot=command-list]]:p-0"
        />
        {audiencesQuery.isPending && (
          <Text muted small className="mt-2">
            Loading audiences…
          </Text>
        )}
        {audiencesQuery.isError && (
          <Text small className="mt-2 text-destructive">
            Unable to load audiences. Close the sheet and try again.
          </Text>
        )}
        <Text muted small className="mt-2">
          Select the specific audiences that should receive this plugin.
          Assignments apply when a device next syncs.
        </Text>
      </div>
      <SheetFooter className="px-6 pb-6">
        <Button
          variant="secondary"
          onClick={() => onOpenChange(false)}
          disabled={mutation.isPending}
        >
          Cancel
        </Button>
        <Button onClick={handleSave} disabled={mutation.isPending}>
          {mutation.isPending ? "Saving..." : "Save assignments"}
        </Button>
      </SheetFooter>
    </>
  );
}
