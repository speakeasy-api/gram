"use client";

import { ChevronDownIcon } from "lucide-react";
import { memo, type FC } from "react";

import type { TextMessagePartComponent } from "@assistant-ui/react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/elements/components/ui/collapsible";
import { ReferenceText } from "@/elements/components/assistant-ui/reference-token";
import { REFERENCE_TOKEN_CLASSES } from "@/elements/lib/reference-token-classes";
import { skillTokensIn } from "@/elements/lib/tool-mentions";
import { cn } from "@/lib/utils";

import {
  splitContextBlocks,
  type ContextBlock,
} from "./user-message-text.helpers";

/**
 * Folds the app-injected `<…context>` block(s) into a collapsed disclosure —
 * the same "expand to inspect" affordance as the assistant's Reasoning trace.
 */
const ContextDisclosure: FC<{ blocks: ContextBlock[] }> = ({ blocks }) => {
  return (
    <Collapsible className="aui-user-context-root mb-2 w-full">
      <CollapsibleTrigger
        className={cn(
          "aui-user-context-trigger group/trigger flex items-center gap-1.5 py-0.5",
          "text-xs text-muted-foreground transition-colors hover:text-foreground",
        )}
      >
        <ChevronDownIcon
          className={cn(
            "aui-user-context-chevron size-3.5 shrink-0 transition-transform",
            "group-data-[state=closed]/trigger:-rotate-90",
          )}
        />
        <span>Additional context</span>
      </CollapsibleTrigger>
      <CollapsibleContent className="aui-user-context-content overflow-hidden data-[state=closed]:animate-collapsible-up data-[state=open]:animate-collapsible-down">
        {/* `w-0 min-w-full`: don't let the (often single-line) context grow the
            shrink-to-fit message bubble — contribute 0 to its intrinsic width,
            then fill the bubble's resolved width and wrap. Keeps the bubble the
            same width open or closed. */}
        <div className="aui-user-context-body mt-1.5 w-0 min-w-full space-y-2 border-l-2 border-border pl-3 text-xs whitespace-pre-line text-muted-foreground">
          {blocks.map((block, i) => (
            <p key={i}>{block.body}</p>
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
};

/**
 * Drop-in replacement for the default user text part. Folds leading
 * `<…context>` blocks into a collapsed disclosure and renders the remaining
 * human-authored text exactly as the assistant-ui default does
 * (`white-space: pre-line`).
 */
const UserMessageTextImpl: TextMessagePartComponent = ({ text }) => {
  const { blocks, rest } = splitContextBlocks(text);
  if (blocks.length === 0) {
    return (
      <p className="aui-user-message-text whitespace-pre-line">
        <ReferenceText text={text} />
      </p>
    );
  }
  // A skill named by a `/skill` token in the text needs no pill: that would
  // read as the same skill attached twice. One that isn't named — a turn
  // persisted before the composer wrote tokens, or sent by another client —
  // would otherwise leave no trace at all, so it gets a label of its own.
  const attachedSkills = blocks.flatMap((block) =>
    block.skillName ? [block.skillName] : [],
  );
  const named = new Set(
    skillTokensIn(rest, attachedSkills).map((name) => name.toLowerCase()),
  );
  const unnamedSkills = attachedSkills.filter(
    (name) => !named.has(name.toLowerCase()),
  );
  const disclosureBlocks = blocks.filter((block) => !block.skillName);
  return (
    <div className="aui-user-message-text-with-context">
      {unnamedSkills.length > 0 && (
        <div className="aui-user-message-skills mb-1.5 flex flex-wrap gap-1">
          {unnamedSkills.map((name) => (
            <span key={name} className={REFERENCE_TOKEN_CLASSES.surface.skill}>
              /{name}
            </span>
          ))}
        </div>
      )}
      {disclosureBlocks.length > 0 ? (
        <ContextDisclosure blocks={disclosureBlocks} />
      ) : null}
      {rest.trim() !== "" && (
        <p className="aui-user-message-text whitespace-pre-line">
          <ReferenceText text={rest} />
        </p>
      )}
    </div>
  );
};

export const UserMessageText = memo(UserMessageTextImpl);
UserMessageText.displayName = "UserMessageText";
