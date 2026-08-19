import { MultiSelect } from "@/components/ui/MultiSelect";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
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

const audienceTypeOptions: {
  value: PluginAudienceKind;
  label: string;
}[] = [
  { value: "everyone", label: "Everyone" },
  { value: "role", label: "Role" },
  { value: "directory_group", label: "Directory group" },
  { value: "directory_attribute", label: "Directory attribute" },
];

function isAudienceType(value: string): value is PluginAudienceKind {
  return audienceTypeOptions.some((type) => type.value === value);
}

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
    icon: principalIcon(principal.kind),
    disabled: true,
  };
}

function availableAudienceOptions(
  audienceType: PluginAudienceKind,
  audiences: PluginAudience[],
) {
  return audiences
    .filter((audience) => audience.kind === audienceType)
    .map((audience) => ({
      label: audience.displayName,
      value: audience.principalUrn,
      description: memberCountDescription(audience.memberCount),
      icon: principalIcon(audience.kind),
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
  const [audienceType, setAudienceType] =
    useState<PluginAudienceKind>("everyone");

  const audienceByUrn = useMemo(() => audienceMapByUrn(audiences), [audiences]);
  const selectedForAudienceType = useMemo(
    () =>
      selected.filter(
        (urn) => audienceKindForPrincipal(urn, audienceByUrn) === audienceType,
      ),
    [audienceByUrn, audienceType, selected],
  );
  const availableOptions = useMemo(
    () => availableAudienceOptions(audienceType, audiences),
    [audienceType, audiences],
  );
  const unavailableOptions = useMemo(
    () => unavailableAudienceOptions(audienceType, selected, audienceByUrn),
    [audienceByUrn, audienceType, selected],
  );
  const options = useMemo(
    () => [...availableOptions, ...unavailableOptions],
    [availableOptions, unavailableOptions],
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

  const handleAudienceSelection = (values: string[]) => {
    setSelected((current) => [
      ...current.filter(
        (urn) => audienceKindForPrincipal(urn, audienceByUrn) !== audienceType,
      ),
      ...values,
    ]);
  };

  const handleLegacySelection = (values: string[]) => {
    setSelected((current) => [
      ...current.filter((urn) => audienceKindForPrincipal(urn, audienceByUrn)),
      ...values,
    ]);
  };

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
        <label
          className="mb-2 block text-sm font-medium"
          htmlFor="audience-type"
        >
          Audience type
        </label>
        <Select
          value={audienceType}
          onValueChange={(value) => {
            if (isAudienceType(value)) setAudienceType(value);
          }}
        >
          <SelectTrigger id="audience-type" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {audienceTypeOptions.map((type) => (
              <SelectItem key={type.value} value={type.value}>
                {type.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <label className="mb-2 mt-5 block text-sm font-medium">
          {
            audienceTypeOptions.find((type) => type.value === audienceType)
              ?.label
          }
        </label>
        <MultiSelect
          key={audienceType}
          options={[{ heading: "Available audiences", options }]}
          defaultValue={selectedForAudienceType}
          onValueChange={handleAudienceSelection}
          placeholder={`Select ${audienceTypeOptions
            .find((type) => type.value === audienceType)
            ?.label.toLowerCase()}`}
          badgeClassName="h-6 gap-1.5 px-1.5 font-sans text-xs normal-case tracking-normal"
          searchable
          hideSelectAll
          modalPopover
          maxCount={20}
          disabled={audiencesQuery.isPending || audiencesQuery.isError}
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
          Choose a type, then select the specific audiences that should receive
          this plugin. Assignments apply when a device next syncs.
        </Text>
        {legacyOptions.length > 0 && (
          <>
            <label className="mb-2 mt-5 block text-sm font-medium">
              Legacy assignments
            </label>
            <MultiSelect
              options={legacyOptions}
              defaultValue={legacyOptions.map((option) => option.value)}
              onValueChange={handleLegacySelection}
              placeholder="No legacy assignments"
              badgeClassName="h-6 gap-1.5 px-1.5 font-sans text-xs normal-case tracking-normal"
              searchable={false}
              hideSelectAll
              modalPopover
              maxCount={20}
            />
            <Text muted small className="mt-2">
              These assignments can be removed but not added again.
            </Text>
          </>
        )}
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
