"use client";

import * as React from "react";
import { useEffect } from "react";
import { useAui } from "@assistant-ui/react";
import { Composer } from "../assistant-ui/thread";
import { ErrorBoundary } from "../assistant-ui/error-boundary";
import { ShadowRoot } from "@/elements/components/ShadowRoot";

/** Marks the shadow host so hosts can reach the input inside it (see
 *  `focusChatComposer`). */
const COMPOSER_HOST_CLASS = "gram-elements-composer-host";

interface ChatComposerProps {
  className?: string;
}

/**
 * Focus the composer's input from outside the shadow root — e.g. the dock's
 * Cmd+/ shortcut. `root` is any ancestor element of the ChatComposer; shadow
 * DOM is queried explicitly because `querySelector` does not cross it.
 */
export function focusChatComposer(root: HTMLElement | null): void {
  root
    ?.querySelector<HTMLElement>(`.${COMPOSER_HOST_CLASS}`)
    ?.shadowRoot?.querySelector<HTMLTextAreaElement>("textarea")
    ?.focus();
}

/**
 * The chat composer on its own, without a message thread.
 *
 * Entry points that start a conversation (the /chat landing, the docked pill)
 * used to hand-roll a textarea and inject the text into the runtime after the
 * fact, which left them without dictation, attachments, tool mentions, or
 * skill context. This renders the SAME composer the thread uses, bound to the
 * surrounding runtime, so those surfaces gain every composer feature — present
 * and future — for free.
 *
 * Requires an ElementsProvider ancestor (for the runtime); its own ShadowRoot
 * carries the Elements stylesheet, exactly as `Chat` does.
 */
/**
 * Drops the draft when a standalone composer goes away. Composer state lives
 * on the shared runtime, so without this a half-typed message follows the user
 * from one entry point to the next (chat home -> project home) as if they had
 * typed it there.
 */
function ClearDraftOnUnmount(): null {
  const aui = useAui();
  useEffect(
    () => () => {
      aui.composer().setText("");
    },
    [aui],
  );
  return null;
}

export const ChatComposer = ({
  className,
}: ChatComposerProps): React.JSX.Element => (
  <ErrorBoundary>
    <ShadowRoot
      hostClassName={COMPOSER_HOST_CLASS}
      hostStyle={{ width: "100%" }}
    >
      <div className={className}>
        <ClearDraftOnUnmount />
        <Composer showThreadAffordances={false} autoFocus={false} />
      </div>
    </ShadowRoot>
  </ErrorBoundary>
);
