/** Pure fuzzy ranking for command palette candidates. */
import type { LauncherCandidate, Verb } from "./candidates/types";

/** Candidates below this fuzzy score are dropped entirely. */
export const MIN_FUZZY = 0.15;
/**
 * Filler words stripped from the query before matching. The launcher's list
 * plus dashboard filler that would otherwise crowd out identifiers. "mcp" is
 * deliberately absent: it distinguishes MCP rows from plugins.
 */
export const STOP_WORDS: ReadonlySet<string> = new Set([
  "the",
  "a",
  "an",
  "i",
  "my",
  "me",
  "to",
  "of",
  "that",
  "just",
  "please",
  "open",
  "launch",
  "run",
  "go",
  "show",
  "find",
  "get",
  "up",
  "it",
  "ve",
  "s",
  "d",
  "ll",
  "re",
  "m",
  "in",
  "on",
  "from",
  "for",
  "with",
  "all",
  "every",
  "everything",
  "any",
  "and",
  "was",
  "were",
  "been",
  "have",
  "had",
  "ive",
  "did",
  "about",
  "at",
  "page",
  "pages",
  "site",
  "sites",
  "stuff",
  "thing",
  "things",
  "read",
  "looked",
  "saw",
  "some",
  "those",
  "these",
  "them",
  "this",
  // Dashboard filler.
  "server",
  "turn",
  "set",
  "switch",
]);

// Combining marks on a Latin base letter ("ë" → "e"). Marks on other scripts
// are letters in their own right (Cyrillic "й" is "и" + breve) and stay.
const LATIN_MARKS_RE = /(?<=\p{Script=Latin})\p{M}+/gu;

/**
 * Lowercase, fold Latin accents, split on anything that is not a letter,
 * combining mark or digit in any script, drop empties. Unicode-aware so a
 * Cyrillic or CJK title tokenises to its words rather than to nothing, and
 * marks that survive folding (Devanagari vowel signs, Arabic harakat) stay
 * attached to their word instead of splitting it.
 */
export function tokens(text: string): string[] {
  return text
    .normalize("NFD")
    .replace(LATIN_MARKS_RE, "")
    .normalize("NFC")
    .toLowerCase()
    .split(/[^\p{L}\p{M}\p{N}]+/u)
    .filter((token) => token.length > 0);
}

const EXACT = 1.0;
const PREFIX_BASE = 0.8;
const PREFIX_LENGTH_WEIGHT = 0.15;
const INITIALS = 0.7;
const CONTAINS = 0.55;
const SUBSEQUENCE_BASE = 0.2;
const SUBSEQUENCE_CONTIGUITY_WEIGHT = 0.2;
const UNMATCHED_PENALTY = 0.5;
const SHORT_TITLE_BONUS = 0.02;
const SHORT_TITLE_LENGTH = 40;

/**
 * Fraction of adjacent matched positions when `token` is matched greedily as
 * a subsequence of `haystack`, or `null` when it is not a subsequence.
 */
function subsequenceContiguity(token: string, haystack: string): number | null {
  let previous = -1;
  let adjacent = 0;
  for (let i = 0; i < token.length; i++) {
    const position = haystack.indexOf(token.charAt(i), previous + 1);
    if (position === -1) return null;
    if (previous !== -1 && position === previous + 1) adjacent++;
    previous = position;
  }
  return token.length > 1 ? adjacent / (token.length - 1) : 1;
}

interface CandidateTerms {
  terms: string[];
  joinedTitle: string;
  initials: string;
}

function candidateTerms(
  c: Pick<LauncherCandidate, "title" | "keywords">,
): CandidateTerms {
  const titleTokens = tokens(c.title);
  return {
    terms: [...titleTokens, ...tokens(c.keywords.join(" "))],
    joinedTitle: titleTokens.join(""),
    initials: titleTokens.map((t) => t.charAt(0)).join(""),
  };
}

/** Best tier score for one query token, or 0 when nothing matches. */
function tokenScore(
  token: string,
  { terms, joinedTitle, initials }: CandidateTerms,
): number {
  let best = 0;
  for (const term of terms) {
    if (term === token) return EXACT;
    if (term.startsWith(token)) {
      best = Math.max(
        best,
        PREFIX_BASE + (PREFIX_LENGTH_WEIGHT * token.length) / term.length,
      );
    } else if (term.includes(token)) {
      best = Math.max(best, CONTAINS);
    }
  }
  if (token.length >= 2 && initials.startsWith(token)) {
    best = Math.max(best, INITIALS);
  }
  if (best === 0 && token.length >= 3) {
    const contiguity = subsequenceContiguity(token, joinedTitle);
    if (contiguity !== null) {
      best = SUBSEQUENCE_BASE + SUBSEQUENCE_CONTIGUITY_WEIGHT * contiguity;
    }
  }
  return best;
}

/**
 * Fuzzy match of `query` against a candidate's title and keywords. An empty
 * query, or one where no token matches at all, scores 0. See the design
 * spec's "Fuzzy prefilter" for the tiers.
 */
export function fuzzyScore(
  query: string,
  c: Pick<LauncherCandidate, "title" | "keywords">,
): number {
  const all = tokens(query);
  if (all.length === 0) return 0;
  const meaningful = all.filter((token) => !STOP_WORDS.has(token));
  const queryTokens = meaningful.length > 0 ? meaningful : all;

  const terms = candidateTerms(c);
  let total = 0;
  let unmatched = false;
  for (const token of queryTokens) {
    const score = tokenScore(token, terms);
    if (score === 0) unmatched = true;
    total += score;
  }
  if (total === 0) return 0;

  let score = total / queryTokens.length;
  if (unmatched) score *= UNMATCHED_PENALTY;
  score +=
    SHORT_TITLE_BONUS * Math.max(0, 1 - c.title.length / SHORT_TITLE_LENGTH);
  return score;
}

interface Prefiltered {
  /** Every candidate at or above `minFuzzy`, fuzzy desc then title asc. */
  all: Array<{ candidate: LauncherCandidate; fuzzy: number }>;
}

function compareTitle(a: LauncherCandidate, b: LauncherCandidate): number {
  return a.title.localeCompare(b.title);
}

export function prefilter(
  query: string,
  candidates: LauncherCandidate[],
  opts: { minFuzzy?: number } = {},
): Prefiltered {
  const minFuzzy = opts.minFuzzy ?? MIN_FUZZY;
  const all = candidates
    .map((candidate) => ({ candidate, fuzzy: fuzzyScore(query, candidate) }))
    .filter((row) => row.fuzzy >= minFuzzy)
    .sort(
      (a, b) => b.fuzzy - a.fuzzy || compareTitle(a.candidate, b.candidate),
    );
  return { all };
}

export interface RankedRow {
  candidate: LauncherCandidate;
  score: number;
  fuzzy: number;
  verb: Verb;
  targetP: number;
}

export function rank(pre: Prefiltered): RankedRow[] {
  return pre.all.map(({ candidate, fuzzy }) => ({
    candidate,
    score: fuzzy,
    fuzzy,
    verb: "open",
    targetP: 0,
  }));
}
