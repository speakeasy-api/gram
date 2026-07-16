import { MultiSelect } from "@/components/ui/multi-select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import type { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";
import { useSetPluginAssignmentsMutation } from "@gram/client/react-query/setPluginAssignments";
import { useMembers } from "@gram/client/react-query/members";
import { useRoles } from "@gram/client/react-query/roles";
import { Button } from "@speakeasy-api/moonshine";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  normalizeToPrincipalUrn,
  principalIcon,
  WILDCARD_PRINCIPAL,
} from "./principals";

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
  const { data: rolesData } = useRoles();
  const { data: membersData } = useMembers();
  const roles = useMemo(() => rolesData?.roles ?? [], [rolesData?.roles]);
  const members = useMemo(
    () => membersData?.members ?? [],
    [membersData?.members],
  );

  const initialUrns = useMemo(
    () => assignments.map((a) => a.principalUrn),
    [assignments],
  );
  const [selected, setSelected] = useState<string[]>(initialUrns);

  // Options provide friendly labels for every principal a plugin can already be
  // assigned to, so current assignments (seeded via defaultValue) render as
  // names rather than raw URNs. New emails are added via the creatable input.
  const options = useMemo(() => {
    const everyone = {
      label: "Everyone",
      value: WILDCARD_PRINCIPAL,
      icon: principalIcon("everyone"),
    };
    const roleOptions = roles.map((role) => ({
      label: role.name,
      value: role.principalUrn,
      icon: principalIcon("role"),
    }));
    const memberOptions = members.map((member) => ({
      label: member.name ? `${member.name} (${member.email})` : member.email,
      value: member.principalUrn,
      icon: principalIcon("user"),
    }));

    // Surface existing email assignments as their own options so they show the
    // address as a label and stay selected. Members already cover user: URNs.
    const knownValues = new Set([
      everyone.value,
      ...roleOptions.map((o) => o.value),
      ...memberOptions.map((o) => o.value),
    ]);
    const emailOptions = initialUrns
      .filter((urn) => urn.startsWith("email:") && !knownValues.has(urn))
      .map((urn) => ({
        label: urn.slice("email:".length),
        value: urn,
        icon: principalIcon("email"),
      }));

    return [
      { heading: "General", options: [everyone] },
      ...(roleOptions.length
        ? [{ heading: "Roles", options: roleOptions }]
        : []),
      ...(memberOptions.length
        ? [{ heading: "Users", options: memberOptions }]
        : []),
      ...(emailOptions.length
        ? [{ heading: "Emails", options: emailOptions }]
        : []),
    ];
  }, [roles, members, initialUrns]);

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
    // Canonicalize the picked values into principal URNs. Anything that is
    // neither a known URN nor a valid email (e.g. a typo typed into the picker)
    // blocks the save so we never persist an assignment that can't deliver.
    const normalized: string[] = [];
    const invalid: string[] = [];
    for (const value of selected) {
      const urn = normalizeToPrincipalUrn(value);
      if (urn) normalized.push(urn);
      else invalid.push(value);
    }
    if (invalid.length > 0) {
      toast.error(`Not a valid role, user, or email: ${invalid.join(", ")}`);
      return;
    }

    mutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        setPluginAssignmentsForm: {
          pluginId,
          principalUrns: Array.from(new Set(normalized)),
        },
      },
    });
  };

  return (
    <>
      <div className="flex-1 overflow-y-auto px-6 py-4">
        <label className="mb-2 block text-sm font-medium">
          Assigned principals
        </label>
        <MultiSelect
          options={options}
          defaultValue={initialUrns}
          onValueChange={setSelected}
          placeholder="Add roles, users, or emails"
          creatable
          searchable
          hideSelectAll
          maxCount={20}
        />
        <Type muted small className="mt-2">
          Type an email and select “Create” to assign it directly. Role and user
          assignments deliver once the recipient runs the device agent.
        </Type>
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
