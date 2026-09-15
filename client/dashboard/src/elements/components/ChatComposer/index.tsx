"use client";

import * as React from "react";
import { useEffect } from "react";
import { useAui } from "@assistant-ui/react";
import { AttachmentDropZone } from "../assistant-ui/attachment-dropzone";
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
      // The host's `isolation: isolate` makes a stacking context but leaves
      // the box static, so the whole shadow subtree paints with in-flow
      // content — beneath any `position: relative` page chrome sharing the
      // ancestor stacking context, avatars included. The context popover
      // can't win that from inside (it is portalled into the shadow root so
      // the scoped styles apply), so the host itself has to sit in the
      // positioned paint layer. `z-index: 1` clears bare `z-auto` chrome
      // while staying under the insights dock (z-30) and dialogs (z-50).
      hostStyle={{ width: "100%", position: "relative", zIndex: 1 }}
    >
      <AttachmentDropZone className={className}>
        <ClearDraftOnUnmount />
        <Composer showThreadAffordances={false} autoFocus={false} />
      </AttachmentDropZone>
    </ShadowRoot>
  </ErrorBoundary>
);
