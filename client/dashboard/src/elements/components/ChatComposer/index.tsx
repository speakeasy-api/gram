"use client";

import * as React from "react";
import { useEffect } from "react";
import { useAui } from "@assistant-ui/react";
import { Composer } from "../assistant-ui/thread";
import { ErrorBoundary } from "../assistant-ui/error-boundary";
import { ShadowRoot } from "@/elements/components/ShadowRoot";
import { COMPOSER_HOST_CLASS } from "@/elements/lib/composerFocus";

interface ChatComposerProps {
  className?: string;
}

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

/**
 * The chat composer on its own, without a message thread.
 *
 * Entry points that start a conversation (the /chat landing, the docked pill)
 * used to hand-roll a textarea and inject the text into the runtime after the
 * fact, which left them without dictation, attachments, tool mentions, or
 * skill context. This renders the SAME composer the thread uses, bound to the
 * surrounding runtime, so those surfaces gain every composer feature.
 *
 * Requires an ElementsProvider ancestor (for the runtime); its own ShadowRoot
 * carries the Elements stylesheet, exactly as `Chat` does.
 */
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
