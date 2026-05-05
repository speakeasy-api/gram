/**
 * Synthetic recall queries across three shape buckets, intended to
 * approximate the phrasings an LLM agent would generate when calling
 * `recall(query)`. Stratified by query length because embedding latency
 * scales with input tokens.
 *
 * Topics are mixed (tech, lifestyle, role, food, family, ops) so the
 * timing isn't biased by a single semantic family.
 */

export type Query = { id: string; text: string };
export type Bucket = "short" | "medium" | "long";

/** Short keyword-style recall, ~3-10 tokens. The dominant agent shape in practice. */
export const short: Query[] = [
  { id: "s01", text: "preferred editor" },
  { id: "s02", text: "user job and company" },
  { id: "s03", text: "favorite framework" },
  { id: "s04", text: "primary programming language" },
  { id: "s05", text: "dietary restrictions" },
  { id: "s06", text: "current city" },
  { id: "s07", text: "preferred database" },
  { id: "s08", text: "marital status" },
  { id: "s09", text: "kids count" },
  { id: "s10", text: "deployment platform" },
];

/** Full-question phrasing, ~20-40 tokens. */
export const medium: Query[] = [
  {
    id: "m01",
    text: "what is the user's preferred backend database for early-stage prototypes",
  },
  { id: "m02", text: "does the user have any pets and how many of each kind" },
  {
    id: "m03",
    text: "what does the user think about typescript versus javascript on the backend",
  },
  {
    id: "m04",
    text: "what is the user's stance on remote work versus going in to an office",
  },
  {
    id: "m05",
    text: "preferred coffee order or general caffeine routine in the morning",
  },
  { id: "m06", text: "what city does the user live in and how is the commute" },
  {
    id: "m07",
    text: "user's hobbies outside of programming and software development",
  },
  {
    id: "m08",
    text: "user's experience level with rust ownership lifetimes and async runtimes",
  },
  {
    id: "m09",
    text: "preferred deployment target when shipping early-stage prototypes to real users",
  },
  {
    id: "m10",
    text: "user's primary fitness routine and how often they train each week",
  },
];

/** Multi-clause exposition, ~80-150 tokens. Less common but worth knowing the upper bound. */
export const long: Query[] = [
  {
    id: "l01",
    text:
      "What is the user's preferred full backend stack including database choice, ORM or " +
      "query builder, web framework, and target deployment platform when building early-stage " +
      "prototypes versus mature production systems at a larger company with more compliance " +
      "requirements? I want to recommend a stack that lines up with their stated preferences.",
  },
  {
    id: "l02",
    text:
      "The user mentioned multiple dietary preferences in past conversations including some " +
      "restrictions related to lactose, allergies they have flagged, and longer-term lifestyle " +
      "choices around alcohol or red meat. What is the most up-to-date picture of their dietary " +
      "constraints so I can suggest a restaurant that fits all of them?",
  },
  {
    id: "l03",
    text:
      "Across previous threads the user has talked about their current role, their team size, " +
      "the company stage they are at, the tools their team uses for project management, and the " +
      "size of the engineering organization. Pull together what we know about their current " +
      "professional context so I can tailor advice on hiring practices appropriately.",
  },
  {
    id: "l04",
    text:
      "The user has discussed several different frameworks for building user interfaces over " +
      "time including ones they like and ones they have explicitly rejected, plus their " +
      "preferences around state management, styling solutions, and component libraries. " +
      "Summarize what we know about their frontend preferences so I can recommend a starter " +
      "template that matches their tastes.",
  },
  {
    id: "l05",
    text:
      "Pull everything we know about the user's family situation including spouse or partner, " +
      "any children with names and ages where shared, pets, recent moves or relocations, and " +
      "broader life events that might be relevant context for understanding their availability " +
      "and energy levels for new commitments at work.",
  },
  {
    id: "l06",
    text:
      "What does the user typically eat across a normal work week including breakfast routines, " +
      "lunch habits, snacks they keep around the house, and dinner patterns? They've mentioned " +
      "trying various dietary approaches over time and I want to suggest a meal plan that aligns " +
      "with what has actually stuck for them rather than what they tried briefly and abandoned.",
  },
  {
    id: "l07",
    text:
      "Reconstruct the user's tooling preferences for software development including their " +
      "preferred shell, terminal emulator, editor, version control workflow, package manager, " +
      "container runtime, and any specific configuration choices they have mentioned strongly " +
      "preferring. I want to recommend a dev environment setup that matches.",
  },
  {
    id: "l08",
    text:
      "Across all past sessions, what has the user said about their travel preferences, the " +
      "places they have visited or want to visit, any frequent-flyer status they have mentioned, " +
      "their preferences for hotels versus alternative lodging, and constraints around their " +
      "ability to travel based on family or work obligations?",
  },
  {
    id: "l09",
    text:
      "What is the full picture of the user's fitness and health context including their " +
      "current training routine, sports they practice, any injuries or limitations they have " +
      "mentioned, dietary supplements or medications relevant to performance, and their stated " +
      "goals for the next few months that should shape any recommendations I make?",
  },
  {
    id: "l10",
    text:
      "Pull together what we know about the user's writing and learning habits including the " +
      "books they have mentioned reading recently, podcasts they listen to, newsletters they " +
      "subscribe to, areas they have said they want to learn more about, and the format they " +
      "prefer for new technical material so I can recommend follow-up reading.",
  },
];

export const allQueries: { bucket: Bucket; query: Query }[] = [
  ...short.map((q) => ({ bucket: "short" as const, query: q })),
  ...medium.map((q) => ({ bucket: "medium" as const, query: q })),
  ...long.map((q) => ({ bucket: "long" as const, query: q })),
];
