import { useAuiState } from "@assistant-ui/react";
import { useRef } from "react";
import {
  excerptFromReadDoc,
  findDocsExcerpts,
  findReadDocs,
  type DocsExcerpt,
} from "@/elements/components/assistant-ui/search-docs-result";

/**
 * The documentation citations for one tool run. Shared with the homepage
 * terminal skin, which renders runs flat but keeps the same sources.
 */
export function useRunCitations(indices: number[]): DocsExcerpt[] {
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
  return citationKey ? excerptsRef.current : [];
}
