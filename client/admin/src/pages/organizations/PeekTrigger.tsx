import { PanelRightIcon } from "lucide-react";
import { createContext, useContext, type JSX, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { AdminOrganization } from "@/lib/gramAdminApi";

import { PEEK_PANEL_ID } from "./PeekPanel";

export type PeekControls = {
  peekedId?: string;
  togglePeek: (org: AdminOrganization) => void;
};

const PeekContext = createContext<PeekControls>({
  togglePeek: () => {},
});

/**
 * Peek state reaches the trigger cells through context, not through the column
 * definitions: the row model memoises on the column array, so a column array
 * rebuilt for each new `peekedId` would rebuild the model with it.
 *
 * The caller memoises `value`, or every trigger on the page re-renders on every
 * render of the list.
 */
export function PeekProvider({
  value,
  children,
}: {
  value: PeekControls;
  children: ReactNode;
}): JSX.Element {
  return <PeekContext.Provider value={value}>{children}</PeekContext.Provider>;
}

// The close path reaches the button through the peeked row and this selector,
// rather than through `querySelector("button")`: later slices put more buttons
// in the row.
export const PEEK_TRIGGER_SELECTOR = "[data-peek-trigger]";

export function PeekTrigger({ org }: { org: AdminOrganization }): JSX.Element {
  const { peekedId, togglePeek } = useContext(PeekContext);
  const isPeeked = peekedId === org.id;

  return (
    <Tooltip>
      {/* The control is an icon on its own, so nothing on screen says what it
          does until the pointer rests on it. The tooltip describes the
          control; it does not name it, and the accessible name below is what
          a screen reader still announces. */}
      <TooltipTrigger asChild>
        <Button
          // Filled while open. Ghost hover is `bg-accent`, and the peeked row
          // is `bg-muted`, and in this theme those two tokens hold the same
          // grey: an open control styled with either disappears into the row
          // it sits in and reads as a hover everywhere else.
          variant={isPeeked ? "default" : "ghost"}
          size="icon-xs"
          data-peek-trigger=""
          // Stable under the state change. A control whose accessible name
          // moves as it is pressed is announced as a different control; the
          // state rides on aria-expanded instead.
          aria-label={`Peek at ${org.name}`}
          aria-expanded={isPeeked}
          // A peeked row always has its panel mounted, and aria-controls
          // pointing at an absent id is invalid.
          aria-controls={isPeeked ? PEEK_PANEL_ID : undefined}
          onClick={() => togglePeek(org)}
        >
          <PanelRightIcon />
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        {isPeeked ? "Close peek" : "Peek without leaving the list"}
      </TooltipContent>
    </Tooltip>
  );
}
