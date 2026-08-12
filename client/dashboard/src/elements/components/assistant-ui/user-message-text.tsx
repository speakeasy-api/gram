"use client";

import { AtSign, ChevronDownIcon } from "lucide-react";
import { memo, type FC } from "react";

import type { TextMessagePartComponent } from "@assistant-ui/react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/elements/components/ui/collapsible";
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
          "text-primary-foreground/70 hover:text-primary-foreground text-xs transition-colors",
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
        <div className="aui-user-context-body border-primary-foreground/30 text-primary-foreground/70 mt-1.5 w-0 min-w-full space-y-2 border-l-2 pl-3 text-xs whitespace-pre-line">
          {blocks.map((block, i) => (
            <p key={i}>{block.body}</p>
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
};

const SkillContextPills: FC<{ blocks: ContextBlock[] }> = ({ blocks }) => (
  <div className="aui-user-skill-context mb-2 flex flex-wrap gap-1">
    {blocks.map((block, index) => (
      <span
        key={`${block.skillName}-${index}`}
        className="border-primary-foreground/30 bg-primary-foreground/10 text-primary-foreground/80 inline-flex max-w-full items-center gap-1 rounded-md border px-2 py-1 text-xs"
      >
        <AtSign className="size-3 shrink-0" />
        <span className="truncate">{block.skillName}</span>
      </span>
    ))}
  </div>
);

/**
 * Drop-in replacement for the default user text part. Folds leading
 * `<…context>` blocks into a collapsed disclosure and renders the remaining
 * human-authored text exactly as the assistant-ui default does
 * (`white-space: pre-line`).
 */
const UserMessageTextImpl: TextMessagePartComponent = ({ text }) => {
  const { blocks, rest } = splitContextBlocks(text);
  if (blocks.length === 0) {
    return <p className="aui-user-message-text whitespace-pre-line">{text}</p>;
  }
  const skillBlocks = blocks.filter((block) => block.skillName);
  const disclosureBlocks = blocks.filter((block) => !block.skillName);
  return (
    <div className="aui-user-message-text-with-context">
      {skillBlocks.length > 0 ? (
        <SkillContextPills blocks={skillBlocks} />
      ) : null}
      {disclosureBlocks.length > 0 ? (
        <ContextDisclosure blocks={disclosureBlocks} />
      ) : null}
      {rest.trim() !== "" && (
        <p className="aui-user-message-text whitespace-pre-line">{rest}</p>
      )}
    </div>
  );
};

export const UserMessageText = memo(UserMessageTextImpl);
UserMessageText.displayName = "UserMessageText";
