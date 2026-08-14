"use client";

import { useMemo, type FC } from "react";

import { useElements } from "@/elements/hooks/useElements";
import { REFERENCE_TOKEN_CLASSES } from "@/elements/lib/reference-token-classes";
import { splitComposerSegments } from "@/elements/lib/tool-mentions";

/**
 * Message text with its `@tool` and `/skill` tokens painted, so a sent message
 * reads the way it did in the composer.
 */
export const ReferenceText: FC<{ text: string; className?: string }> = ({
  text,
  className,
}) => {
  const { config, mcpTools } = useElements();
  const skillNames = useMemo(
    () => (config.composer?.skillContext?.skills ?? []).map((s) => s.name),
    [config.composer?.skillContext?.skills],
  );
  const segments = useMemo(
    () => splitComposerSegments(text, mcpTools, skillNames),
    [text, mcpTools, skillNames],
  );

  return (
    <span className={className}>
      {segments.map((segment, index) => (
        <span
          // Segments are positional runs of one string, so the index IS the
          // identity here.
          key={index}
          className={REFERENCE_TOKEN_CLASSES.surface[segment.kind]}
        >
          {segment.text}
        </span>
      ))}
    </span>
  );
};
