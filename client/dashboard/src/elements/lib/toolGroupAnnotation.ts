/** The minimal shape of a message part this module needs. */
export interface AnnotatablePart {
  type?: string;
  text?: string;
}

/**
 * -ing words that open a sentence without being a doing-phrase, so a segment
 * starting with one must not be mistaken for a heading ("Interesting — all
 * activity sits in the last two days").
 */
const NOT_A_GERUND = new Set([
  "interesting",
  "something",
  "nothing",
  "anything",
  "everything",
  "during",
]);

/**
 * Announcement openers the model keeps prefixing despite instructions — the
 * clause after them is the actual heading material.
 */
const FIRST_PERSON_OPENER =
  /^(?:now\s+)?(?:i'?ll|i\s+will|i'?m\s+going\s+to|i\s+am\s+going\s+to|let\s+me|let'?s)\s+/i;

/** Filler between the opener and the verb: "Let me now check…". */
const OPENER_FILLER = /^(?:now|just|first|also|then|quickly|simply)\s+/i;

/** "Let me go ahead and check…" → "check…". */
const GO_AHEAD = /^go\s+ahead\s+and\s+/i;

/**
 * gerundize turns a base verb into its -ing form: pull → pulling, analyze →
 * analyzing, get → getting, tie → tying. Heuristic, but the verbs that open
 * these announcements are short and regular.
 */
function gerundize(verb: string): string {
  const v = verb.toLowerCase();
  if (v.endsWith("ing")) return v;
  if (v.endsWith("ie")) return `${v.slice(0, -2)}ying`;
  if (v.endsWith("e") && !v.endsWith("ee")) return `${v.slice(0, -1)}ing`;
  // Double a final consonant on short consonant-vowel-consonant verbs (get →
  // getting, scan → scanning) — longer verbs mostly don't double (gather).
  if (v.length <= 4 && /[^aeiou][aeiou][bcdfghklmnprstvz]$/.test(v)) {
    return `${v}${v[v.length - 1]}ing`;
  }
  return `${v}ing`;
}

const startsWithGerund = (segment: string): boolean => {
  const word = segment.match(/^([A-Za-z]+)/)?.[1]?.toLowerCase() ?? "";
  return word.endsWith("ing") && word.length > 4 && !NOT_A_GERUND.has(word);
};

/**
 * normalizeHeading rewrites the model's narration into the doing-phrase the
 * header wants. The prompt asks for "Pulling risk findings by detector" but
 * the model habitually answers "I'll pull the risk findings. Pulling risk
 * findings by detector" or just "I'll pull the risk findings" — so the display
 * enforces the vernacular deterministically instead of hoping:
 *
 * 1. If any sentence starts with a doing-phrase, use the last such sentence
 *    (the model announces, then labels — the label comes last).
 * 2. Else, strip a first-person opener ("I'll", "Let me") off the last
 *    sentence that has one and conjugate its verb: "I'll pull the risk
 *    findings" → "Pulling the risk findings".
 * 3. Else, pass the text through untouched.
 */
export function normalizeHeading(text: string): string {
  const cleaned = text.trim().replace(/\s+/g, " ");
  if (cleaned === "") return "";

  const segments = cleaned
    .split(/(?<=[.!?])\s+/)
    .map((segment) => segment.trim())
    .filter(Boolean);

  const gerundSegment = segments.filter(startsWithGerund).pop();
  const finish = (segment: string): string =>
    segment.replace(/[.!?]+$/, "").trim();

  if (gerundSegment) return finish(gerundSegment);

  const announced = segments
    .filter((segment) => FIRST_PERSON_OPENER.test(segment))
    .pop();
  if (announced) {
    let rest = announced
      .replace(FIRST_PERSON_OPENER, "")
      .replace(OPENER_FILLER, "")
      .replace(GO_AHEAD, "");
    const verb = rest.match(/^([A-Za-z]+)/)?.[1];
    if (verb) {
      rest = rest.slice(verb.length);
      const conjugated = gerundize(verb);
      return finish(
        `${conjugated[0]!.toUpperCase()}${conjugated.slice(1)}${rest}`,
      );
    }
  }

  return finish(cleaned);
}

/**
 * toolGroupAnnotation returns the assistant's own description of what a tool
 * group is doing: the text of the message part immediately preceding it.
 *
 * A response that calls tools carries its narration in the same message —
 * `{ text: "Pulling the last 30 days of usage", tool_calls: [...] }` — so that
 * text IS the description of those calls. It is shown as the group's label
 * rather than as prose above it, which is why the same predicate has to decide
 * both (see `isToolGroupAnnotation`). The raw text is normalized into heading
 * form on the way through (see `normalizeHeading`).
 *
 * Returns "" when there is no such text: a group that opens a message, or one
 * that follows another group's results.
 */
export function toolGroupAnnotation(
  parts: readonly AnnotatablePart[],
  startIndex: number,
): string {
  const previous = parts[startIndex - 1];
  if (previous?.type !== "text") return "";

  return normalizeHeading(previous.text ?? "");
}

/**
 * isToolGroupAnnotation reports whether a text part is serving as the label of
 * the tool group that follows it, and so should not also be rendered as prose.
 * The counterpart to {@link toolGroupAnnotation} — they must agree, or the
 * text either appears twice or vanishes.
 */
export function isToolGroupAnnotation(
  parts: readonly AnnotatablePart[],
  index: number,
): boolean {
  if (parts[index]?.type !== "text") return false;
  if (parts[index + 1]?.type !== "tool-call") return false;
  return toolGroupAnnotation(parts, index + 1) !== "";
}

/**
 * A turn that iterates — call tools, narrate the adjustment, call tools again —
 * produces a CHAIN of tool groups separated only by annotation text. Rendering
 * a headed row per group turns one question into a wall of micro-steps
 * ("Checking filter format", "Paging without filters…", 25 rows deep), so the
 * chain is presented as ONE group: labeled by its opening (goal-level) line,
 * with the intermediate steps revealed on expand. These helpers find the
 * chain's bounds from any member group.
 */
export function toolChainStart(
  parts: readonly AnnotatablePart[],
  startIndex: number,
): number {
  let start = startIndex;
  while (
    start >= 2 &&
    isToolGroupAnnotation(parts, start - 1) &&
    parts[start - 2]?.type === "tool-call"
  ) {
    let runStart = start - 2;
    while (runStart > 0 && parts[runStart - 1]?.type === "tool-call") {
      runStart--;
    }
    start = runStart;
  }
  return start;
}

/** The inclusive end of the chain containing the run ending at `endIndex`. */
export function toolChainEnd(
  parts: readonly AnnotatablePart[],
  endIndex: number,
): number {
  let end = endIndex;
  while (
    isToolGroupAnnotation(parts, end + 1) &&
    parts[end + 2]?.type === "tool-call"
  ) {
    let runEnd = end + 2;
    while (parts[runEnd + 1]?.type === "tool-call") {
      runEnd++;
    }
    end = runEnd;
  }
  return end;
}
