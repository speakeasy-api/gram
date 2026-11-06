import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";

import type { ReactNode } from "react";

/**
 * The acknowledgement a grant of sensitive access waits for. The role editor,
 * Manage Roles and Grant Access all ask it the same way; only the sentence
 * naming who gets the access differs. The sentence is the checkbox's
 * accessible name, so a screen reader hears who the access is for.
 */
export function ConfirmSensitiveAccess({
  checked,
  onCheckedChange,
  children,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  /** "I reviewed the sensitive permissions above and want … to have them." */
  children: ReactNode;
}): JSX.Element {
  return (
    <label className="flex cursor-pointer items-start gap-3 text-left">
      <Checkbox
        checked={checked}
        onCheckedChange={(next) => onCheckedChange(next === true)}
        className="mt-0.5"
      />
      <Text variant="body" className="text-sm">
        {children}
      </Text>
    </label>
  );
}
