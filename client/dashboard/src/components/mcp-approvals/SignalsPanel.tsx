import { Heading } from "@/components/ui/Heading";
import {
  MoreToggle,
  useCollapsedPreview,
} from "@/components/ui/collapsible-preview";
import { cn } from "@/lib/utils";
import { useId } from "react";
import {
  countByTone,
  evidenceSectionAnchor,
  type EvidenceSectionId,
  type EvidenceSignal,
  type SignalTone,
} from "./signals";

const SECTION_LABELS: Record<EvidenceSectionId, string> = {
  trust: "Identity",
  handover: "What it asks for",
  capabilities: "Tools",
  usage: "Usage",
  maturity: "Maturity",
  advisories: "Advisories",
};

const TONE_ORDER: SignalTone[] = ["concern", "watch", "unknown", "clean"];

const TONE_HEADINGS: Record<SignalTone, string> = {
  concern: "Concerns",
  watch: "Notable",
  unknown: "Not established",
  clean: "Checked, nothing found",
};

/**
 * The same tones as a count beside the panel heading. Shorter than the group
 * headings, which read as sentences rather than as units: "2 Checked, nothing
 * found" is not a quantity of anything.
 */
const TONE_TALLY: Record<SignalTone, string> = {
  concern: "concerns",
  watch: "notable",
  unknown: "unknown",
  clean: "checked clean",
};

/**
 * Color carries rank, and only downward. Concerns and notable findings are
 * tinted; unknowns are muted and dashed, the panel's long-standing visual for
 * "we could not find out"; a clean check gets no color at all. Nothing on
 * this page is ever green — a check that ran is a fact about the check, and
 * green would make it read as a verdict about the server.
 */
const TONE_HEADING_CLASS: Record<SignalTone, string> = {
  concern: "text-destructive",
  watch: "text-warning",
  unknown: "text-muted-foreground",
  clean: "text-muted-foreground",
};

const TONE_MARKER_CLASS: Record<SignalTone, string> = {
  concern: "bg-destructive",
  watch: "bg-warning",
  unknown: "border-muted-foreground border border-dashed",
  clean: "bg-muted-foreground/40",
};

/** How many clean checks show before the rest collapses behind a toggle. */
const CLEAN_PREVIEW_COUNT = 0;

/**
 * What stands out, above the evidence that shows its working.
 *
 * Everything here is derived from the same document the sections below
 * render — this panel adds no facts, it only puts them in order of
 * consequence and says which section to read for each. The page used to
 * scatter the same findings across eleven small asides inside whichever
 * question raised them, in reading order rather than in order of what a
 * reviewer needs to decide.
 */
export function SignalsPanel({
  signals,
}: {
  signals: EvidenceSignal[];
}): JSX.Element | null {
  if (signals.length === 0) return null;

  const counts = countByTone(signals);

  return (
    <section className="space-y-2">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        {/* Sized with the evidence questions below rather than above them:
            this panel is their summary, not a rank up from them. */}
        <Heading variant="h2" className="text-lg font-thin">
          What stands out
        </Heading>
        <SignalTally counts={counts} />
      </div>
      <div className="border-border divide-border divide-y border">
        {TONE_ORDER.map((tone) => (
          <SignalGroup
            key={tone}
            tone={tone}
            signals={signals.filter((signal) => signal.tone === tone)}
          />
        ))}
      </div>
    </section>
  );
}

/**
 * The counts beside the heading, so the shape of the list is legible before
 * any of it is read. Tones with nothing in them are left out rather than
 * shown as a zero: "0 concerns" is the reassurance this page must not give.
 */
function SignalTally({
  counts,
}: {
  counts: Record<SignalTone, number>;
}): JSX.Element {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      {TONE_ORDER.filter((tone) => counts[tone] > 0).map((tone) => (
        <span
          key={tone}
          className={cn("text-eyebrow", TONE_HEADING_CLASS[tone])}
        >
          {counts[tone]} {TONE_TALLY[tone]}
        </span>
      ))}
    </div>
  );
}

function SignalGroup({
  tone,
  signals,
}: {
  tone: SignalTone;
  signals: EvidenceSignal[];
}): JSX.Element | null {
  // Clean checks are the record of what was looked at, not part of the
  // decision, so they stay behind a toggle rather than padding the list a
  // reviewer is scanning for problems.
  const { collapsible, expanded, toggle, visible } = useCollapsedPreview(
    signals,
    tone === "clean" ? CLEAN_PREVIEW_COUNT : signals.length,
  );
  const listId = useId();

  if (signals.length === 0) return null;

  return (
    <div className="px-3 py-2">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3">
        <h3 className={cn("text-eyebrow", TONE_HEADING_CLASS[tone])}>
          {TONE_HEADINGS[tone]}
        </h3>
        {collapsible && (
          <MoreToggle
            expanded={expanded}
            onToggle={toggle}
            collapsedLabel={`Show ${signals.length}`}
            controlId={listId}
          />
        )}
      </div>
      {visible.length > 0 && (
        <ul id={listId} className="mt-1.5 space-y-1.5">
          {visible.map((signal) => (
            <SignalRow key={signal.id} signal={signal} />
          ))}
        </ul>
      )}
    </div>
  );
}

function SignalRow({ signal }: { signal: EvidenceSignal }): JSX.Element {
  return (
    <li className="flex items-baseline gap-2 text-xs">
      {/* A square, not a dot: the page has no round corners anywhere else,
          and the marker has to read as a rank rather than as a bullet. */}
      <span
        aria-hidden
        className={cn("mt-1 size-1.5 shrink-0", TONE_MARKER_CLASS[signal.tone])}
      />
      <p className="min-w-0 flex-1">
        <span className="font-medium">{signal.headline}</span>
        {signal.detail && (
          <span className="text-muted-foreground"> {signal.detail}</span>
        )}
      </p>
      {signal.section && (
        // Where to read the working, not a call to action: muted until
        // hovered, and never wrapping, because a two-line link in the gutter
        // of every row is what made this list look like a table of contents.
        <a
          href={`#${evidenceSectionAnchor(signal.section)}`}
          className="text-muted-foreground hover:text-foreground hover:decoration-foreground shrink-0 whitespace-nowrap underline decoration-dotted underline-offset-2"
        >
          {SECTION_LABELS[signal.section]}
        </a>
      )}
    </li>
  );
}
