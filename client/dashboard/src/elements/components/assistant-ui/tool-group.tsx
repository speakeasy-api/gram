import { useAuiState } from "@assistant-ui/react";
import { useMemo, useRef, type FC, type PropsWithChildren } from "react";
import { useElements } from "@/elements/hooks/useElements";
import { humanizeToolName } from "@/elements/lib/humanize";
import {
  isToolCallAnnotation,
  toolCallAnnotationTitle,
  trailingAnnotationLine,
} from "@/elements/lib/toolCallAnnotation";
import { ToolUIGroup } from "@/elements/components/ui/tool-ui";
import { DocsCitations } from "@/elements/components/assistant-ui/docs-citations";
import {
  excerptFromReadDoc,
  findDocsExcerpts,
  findReadDocs,
  type DocsExcerpt,
} from "@/elements/components/assistant-ui/search-docs-result";

/**
 * Renders one tool run: a cluster of tool-call parts plus the terse
 * annotation text parts the assistant emits before each batch (see
 * messagePartGrouping.ts). The heading is the latest annotation in the run,
 * so it advances as the investigation progresses; the annotations' prose
 * renders are suppressed in thread.tsx.
 */
export const ToolGroup: FC<PropsWithChildren<{ indices: number[] }>> = ({
  children,
  indices,
}) => {
  const toolCount = useAuiState(({ message }) => {
    let count = 0;
    for (const i of indices) {
      if (message.parts[i]?.type === "tool-call") count++;
    }
    return count;
  });

  const firstToolName = useAuiState(({ message }) => {
    for (const i of indices) {
      const part = message.parts[i];
      if (part?.type === "tool-call") return part.toolName;
    }
    return undefined;
  });

  const anyMessagePartsAreRunning = useAuiState(({ message }) => {
    for (const i of indices) {
      if (message.parts[i]?.status?.type === "running") return true;
    }
    return false;
  });

  // While the message is still streaming and nothing has been emitted after
  // this run, the next annotation may still replace the heading — keep the
  // group in its running state so the swap shimmers in instead of snapping
  // from a "done" state.
  const isTrailingWhileStreaming = useAuiState(({ message }) => {
    if (message.status?.type !== "running") return false;
    const lastIndex = indices[indices.length - 1] ?? -1;
    for (let i = lastIndex + 1; i < message.parts.length; i++) {
      const part = message.parts[i];
      // An empty text part or a lone annotation (the next batch's heading,
      // mid-flight) doesn't end the run.
      if (
        part?.type === "text" &&
        (part.text.trim().length === 0 || isToolCallAnnotation(part.text))
      ) {
        continue;
      }
      return false;
    }
    return true;
  });

  const isRunning = anyMessagePartsAreRunning || isTrailingWhileStreaming;

  // Latest annotation in the run wins — the heading tracks where the
  // investigation currently is. Mixed prose+annotation blocks stay outside
  // the run (only pure annotations join it), so also check the part just
  // before the run for a trailing annotation line.
  const annotation = useAuiState(({ message }) => {
    for (let k = indices.length - 1; k >= 0; k--) {
      const part = message.parts[indices[k]!];
      if (part?.type === "text" && isToolCallAnnotation(part.text)) {
        return toolCallAnnotationTitle(part.text);
      }
    }
    const first = indices[0] ?? 0;
    const prev = first > 0 ? message.parts[first - 1] : undefined;
    if (prev?.type === "text") {
      const line = trailingAnnotationLine(prev.text);
      if (line) return toolCallAnnotationTitle(line);
    }
    return undefined;
  });

  // Documentation citations render outside the collapsible group: the group
  // hides the mechanics of a run by default, but the sources behind an answer
  // are part of the answer and a reader must see them without expanding
  // anything.
  //
  // The selector returns a key rather than the array it found, because
  // useAuiState compares snapshots by identity and a fresh array every render
  // would never settle. The excerpts themselves ride a ref, which the key
  // invalidates whenever the run's citations change.
  const excerptsRef = useRef<DocsExcerpt[]>([]);
  const citationKey = useAuiState(({ message }) => {
    // The guides this run opened. A search returns candidates, not an answer —
    // a turn typically searches several times, and a generic query matches
    // providers the question never mentioned. Only once the assistant opens a
    // guide has it settled on one, so a run that searched without reading
    // shows nothing rather than a card per candidate.
    const opened = indices.flatMap((i) => {
      const part = message.parts[i];
      return part?.type === "tool-call" ? findReadDocs(part.result) : [];
    });
    if (opened.length === 0) {
      excerptsRef.current = [];
      return "";
    }

    // The search that surfaced a guide usually ran in an earlier run, so its
    // excerpt is looked up across the whole message. A guide opened by URI
    // without any search behind it has no excerpt to find, and falls back to
    // the citation header the guide carries.
    const searched = new Map<string, DocsExcerpt>();
    for (const part of message.parts) {
      if (part.type !== "tool-call") continue;
      for (const excerpt of findDocsExcerpts(part.result)) {
        if (!searched.has(excerpt.uri)) searched.set(excerpt.uri, excerpt);
      }
    }

    const cited = opened.map(
      (doc) => searched.get(doc.uri) ?? excerptFromReadDoc(doc),
    );
    excerptsRef.current = cited;
    // Keyed on everything the card renders, not just identity: a citation can
    // be replaced by a richer one — a header-derived fallback superseded by the
    // search excerpt that names it — without its URI or heading changing.
    return cited
      .map((e) =>
        [
          e.uri,
          e.heading,
          e.title,
          e.source,
          e.docs_url,
          e.excerpt.length,
        ].join("|"),
      )
      .join(",");
  });
  const excerpts = citationKey ? excerptsRef.current : [];

  const { config } = useElements();
  const defaultExpanded = config.tools?.expandToolGroupsByDefault ?? false;

  const groupTitle = useMemo(() => {
    if (annotation) return annotation;
    if (toolCount === 1 && firstToolName) {
      const name = humanizeToolName(firstToolName);
      return anyMessagePartsAreRunning ? `Calling ${name}...` : `Ran ${name}`;
    }
    return anyMessagePartsAreRunning
      ? `Running ${toolCount} tools...`
      : `Ran ${toolCount} tools`;
  }, [annotation, toolCount, firstToolName, anyMessagePartsAreRunning]);

  const status = isRunning ? ("running" as const) : ("complete" as const);

  // If there's a custom component for the single tool, render children
  // directly — but keep the annotation visible, since AssistantText has
  // suppressed its prose render.
  if (
    toolCount === 1 &&
    firstToolName &&
    config.tools?.components?.[firstToolName]
  ) {
    return (
      <>
        {annotation && (
          <div className="my-1 text-sm text-muted-foreground">{annotation}</div>
        )}
        {children}
        {excerpts.length > 0 && (
          <DocsCitations excerpts={excerpts} className="my-2 border" />
        )}
      </>
    );
  }

  // Single and multiple tool calls must share one wrapper element type —
  // diverging branches would remount every card when a streaming turn grows
  // the group.
  return (
    <div className="my-4 w-full max-w-xl">
      <ToolUIGroup
        title={groupTitle}
        status={status}
        defaultExpanded={defaultExpanded}
      >
        {children}
      </ToolUIGroup>
      {excerpts.length > 0 && (
        <DocsCitations excerpts={excerpts} className="mt-2 border" />
      )}
    </div>
  );
};
