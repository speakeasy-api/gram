import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { ChevronDown, Loader2 } from "lucide-react";

/**
 * The single suppression entry point: a "Suppress" menu offering the two ways
 * a set of findings can be hidden — a one-off manual suppression of the
 * current findings, or an exclusion rule that also suppresses matching
 * findings going forward. Shared by the signal drawer and the signal list's
 * multi-select toolbar so the affordance reads identically everywhere.
 */
export function SuppressMenu({
  variant,
  size,
  busy = false,
  onSuppressOnce,
  onCreateRule,
}: {
  variant: "primary" | "secondary" | "tertiary";
  size?: "sm";
  /** Disables the menu and shows a spinner while either action is running. */
  busy?: boolean;
  /** Manually suppress the findings in scope (excluded_at, reason manual). */
  onSuppressOnce: () => void;
  /** Take the user to exclusion rule creation for the findings in scope. */
  onCreateRule: () => void;
}): JSX.Element {
  return (
    // Default (modal) menu on purpose: the drawer usage renders inside a
    // modal Sheet, which puts pointer-events: none on the body — a non-modal
    // menu portaled there would render but never receive clicks.
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant={variant} size={size} disabled={busy} aria-busy={busy}>
          {busy && (
            <Button.LeftIcon>
              <Loader2 className="size-4 animate-spin" />
            </Button.LeftIcon>
          )}
          <Button.Text>Suppress</Button.Text>
          <Button.RightIcon>
            <ChevronDown className="size-4" />
          </Button.RightIcon>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={onSuppressOnce}>
          Suppress Once
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={onCreateRule}>Create Rule</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
