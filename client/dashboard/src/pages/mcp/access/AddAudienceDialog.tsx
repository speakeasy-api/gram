import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { Text } from "@/components/ui/Text";
import type { AudienceOption } from "@gram/client/models/components/audienceoption.js";
import { useAudienceOptions } from "@gram/client/react-query/audienceOptions.js";
import { Loader2 } from "lucide-react";
import { useMemo, useState, type JSX } from "react";
import { audienceIcon, OPTION_GROUPS } from "./serverAudience";

/**
 * Adds principals to a resource's audience. Deliberately does only that: the
 * level each one gets is changed on its row afterwards, so the dialog stays a
 * picker rather than a second place to edit access.
 */
export function AddAudienceDialog({
  title,
  description,
  kinds,
  alreadyAdded,
  pending,
  onAdd,
  onClose,
}: {
  title: string;
  description: string;
  /** Which kinds of principal this dialog offers. */
  kinds: AudienceOption["kind"][];
  alreadyAdded: string[];
  pending: boolean;
  onAdd: (principalUrns: string[]) => void;
  onClose: () => void;
}): JSX.Element {
  const { data, isLoading } = useAudienceOptions();
  const [selected, setSelected] = useState<string[]>([]);

  const groups = useMemo(() => {
    const added = new Set(alreadyAdded);
    return OPTION_GROUPS.filter((group) => kinds.includes(group.kind))
      .map((group) => ({
        heading: group.heading,
        icon: audienceIcon(group.kind),
        options: (data?.options ?? [])
          .filter((option) => option.kind === group.kind)
          .map((option) => ({
            label: option.displayName,
            value: option.principalUrn,
            description: added.has(option.principalUrn)
              ? "Already has access"
              : option.description,
            disabled: added.has(option.principalUrn),
          })),
      }))
      .filter((group) => group.options.length > 0);
  }, [data?.options, kinds, alreadyAdded]);

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content className="sm:max-w-lg">
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{description}</Dialog.Description>
        </Dialog.Header>

        <div className="py-2">
          {isLoading ? (
            <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
          ) : groups.length === 0 ? (
            <Text muted small>
              Nothing to add. Directory groups and attributes appear here once
              an identity provider is connected.
            </Text>
          ) : (
            <MultiSelect
              options={groups}
              value={selected}
              onValueChange={setSelected}
              // The dialog locks page scrolling, which also swallows wheel
              // events in a non-modal popover portalled outside it.
              modalPopover
              hideSelectAll
              placeholder="Search"
              // The list belongs to the field above it, so it lines up with
              // the trigger rather than sizing itself to its longest row.
              popoverClassName="w-[var(--radix-popover-trigger-width)] min-w-0"
            />
          )}
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={selected.length === 0 || pending}
            onClick={() => onAdd(selected)}
          >
            {pending && (
              <Button.LeftIcon>
                <Loader2 className="h-4 w-4 animate-spin" />
              </Button.LeftIcon>
            )}
            <Button.Text>
              {selected.length > 1 ? `Add ${selected.length}` : "Add"}
            </Button.Text>
          </Button>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
