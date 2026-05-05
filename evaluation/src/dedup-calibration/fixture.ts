/**
 * Calibration fixture: 161 labelled sentence pairs.
 *
 * Used to pick a dedup-cosine threshold for `text-embedding-3-small`
 * in the Assistant Memory RFC. Goal: separate "true duplicate" pairs
 * (a `remember()` call would be redundant) from "related but distinct"
 * pairs (different facts about the same topic — must NOT dedup) and
 * "unrelated" pairs (different topics — sets the noise floor).
 *
 * Synthesized to look like agent-emitted assistant memories:
 * short, third-person, user-anchored, single-fact.
 */

export type Pair = { a: string; b: string };

/** 81 pairs. Two unrelated facts. Sets cosine noise floor for unrelated English. */
export const unrelated: Pair[] = [
  {
    a: "The user works at Acme as a senior staff engineer.",
    b: "The user is allergic to shellfish.",
  },
  {
    a: "The user holds a PhD in computer science.",
    b: "The user enjoys hiking on weekends.",
  },
  {
    a: "The user prefers Neovim as their text editor.",
    b: "The user has two children, ages 5 and 8.",
  },
  {
    a: "The user lives in Brooklyn, New York.",
    b: "The user is learning to play classical guitar.",
  },
  {
    a: "The user follows a vegetarian diet.",
    b: "The user drives a 2019 Subaru Outback.",
  },
  {
    a: "The user runs marathons twice a year.",
    b: "The user works on payment-processing infrastructure.",
  },
  {
    a: "The user is married with one daughter.",
    b: "The user prefers TypeScript over JavaScript.",
  },
  {
    a: "The user studied mechanical engineering at MIT.",
    b: "The user owns a golden retriever named Max.",
  },
  {
    a: "The user takes lisinopril daily for blood pressure.",
    b: "The user manages a team of seven engineers.",
  },
  {
    a: "The user is fluent in Spanish and Portuguese.",
    b: "The user uses Linear for project tracking.",
  },
  {
    a: "The user grew up in rural Vermont.",
    b: "The user prefers dark mode in all applications.",
  },
  {
    a: "The user is currently writing a novel.",
    b: "The user uses an iPhone 15 Pro.",
  },
  {
    a: "The user has a peanut allergy.",
    b: "The user works remotely from a home office.",
  },
  {
    a: "The user plays in a community orchestra.",
    b: "The user prefers PostgreSQL over MySQL.",
  },
  {
    a: "The user is training for a triathlon next summer.",
    b: "The user has a younger brother named Tom.",
  },
  {
    a: "The user is a licensed private pilot.",
    b: "The user prefers tea over coffee.",
  },
  {
    a: "The user owns a small vineyard in Sonoma.",
    b: "The user uses Vim keybindings in VS Code.",
  },
  {
    a: "The user lived in Tokyo for three years.",
    b: "The user is gluten-intolerant.",
  },
  {
    a: "The user volunteers at a local animal shelter.",
    b: "The user codes primarily in Rust and Go.",
  },
  {
    a: "The user is recovering from knee surgery.",
    b: "The user holds an MBA from Wharton.",
  },
  {
    a: "The user collects vintage typewriters.",
    b: "The user has a daily 30-minute meditation practice.",
  },
  {
    a: "The user is the only child of two physicians.",
    b: "The user prefers analog film photography.",
  },
  {
    a: "The user attended Stanford for undergrad.",
    b: "The user has a severe dust mite allergy.",
  },
  {
    a: "The user is a competitive chess player rated 1900.",
    b: "The user uses tmux for terminal multiplexing.",
  },
  {
    a: "The user owns a Tesla Model 3.",
    b: "The user has been sober for four years.",
  },
  {
    a: "The user speaks intermediate Mandarin.",
    b: "The user prefers using JetBrains IDEs.",
  },
  {
    a: "The user lives in a fourth-floor walkup in Manhattan.",
    b: "The user is a certified scuba diver.",
  },
  {
    a: "The user runs a small Etsy shop selling pottery.",
    b: "The user uses GitHub Copilot daily.",
  },
  {
    a: "The user has type-1 diabetes diagnosed at age 12.",
    b: "The user is a die-hard fan of the Boston Celtics.",
  },
  {
    a: "The user grew up speaking Tagalog at home.",
    b: "The user owns a 1969 Triumph Bonneville motorcycle.",
  },
  {
    a: "The user works at a Series B fintech startup.",
    b: "The user prefers handwritten notes over digital ones.",
  },
  {
    a: "The user just adopted a rescue cat named Pippin.",
    b: "The user uses Obsidian for knowledge management.",
  },
  {
    a: "The user is partway through a sabbatical year.",
    b: "The user is a member of a local rock-climbing gym.",
  },
  {
    a: "The user finished training for a half marathon.",
    b: "The user prefers reading non-fiction over fiction.",
  },
  {
    a: "The user maintains a popular open-source project.",
    b: "The user has a grass-pollen allergy in spring.",
  },
  {
    a: "The user is engaged to be married next September.",
    b: "The user uses pnpm instead of npm for Node projects.",
  },
  {
    a: "The user has a side business consulting on SEO.",
    b: "The user keeps kosher.",
  },
  {
    a: "The user lives within walking distance of work.",
    b: "The user is reading a book on Stoic philosophy.",
  },
  {
    a: "The user took up oil painting last year.",
    b: "The user prefers AWS over GCP for cloud infrastructure.",
  },
  {
    a: "The user has a long commute by ferry.",
    b: "The user is an only child raised in Ohio.",
  },
  {
    a: "The user worked at Google for six years before this role.",
    b: "The user is allergic to penicillin.",
  },
  {
    a: "The user is a competitive swimmer in masters category.",
    b: "The user prefers Notion to Confluence for docs.",
  },
  {
    a: "The user co-parents two teenagers post-divorce.",
    b: "The user uses Jujutsu over Git for version control.",
  },
  {
    a: "The user once interned at NASA.",
    b: "The user follows a low-FODMAP diet for IBS.",
  },
  {
    a: "The user lives with a chronic migraine condition.",
    b: "The user prefers Sublime Text for quick edits.",
  },
  {
    a: "The user holds a CCNA certification.",
    b: "The user has visited 38 countries so far.",
  },
  {
    a: "The user just bought a fixer-upper in upstate NY.",
    b: "The user prefers Postman over Insomnia.",
  },
  {
    a: "The user serves on the board of a non-profit.",
    b: "The user is allergic to bee stings.",
  },
  {
    a: "The user trains in Brazilian jiu-jitsu twice weekly.",
    b: "The user prefers GitLab CI over GitHub Actions.",
  },
  {
    a: "The user is a first-generation immigrant from Ukraine.",
    b: "The user uses Hammerspoon for macOS automation.",
  },
  {
    a: "The user keeps a flock of backyard chickens.",
    b: "The user prefers Datadog over New Relic.",
  },
  {
    a: "The user is studying for the bar exam.",
    b: "The user has a corn allergy.",
  },
  {
    a: "The user lives off-grid on a small farm in Maine.",
    b: "The user prefers Slack over Microsoft Teams.",
  },
  {
    a: "The user is recently widowed.",
    b: "The user uses kitty as their primary terminal emulator.",
  },
  {
    a: "The user has worked in academia for 15 years.",
    b: "The user prefers boutique hotels over chains.",
  },
  {
    a: "The user runs a popular newsletter on AI ethics.",
    b: "The user has lactose intolerance.",
  },
  {
    a: "The user is a black belt in taekwondo.",
    b: "The user prefers Helm over raw Kubernetes manifests.",
  },
  {
    a: "The user just published their first novel.",
    b: "The user has a soy allergy.",
  },
  {
    a: "The user is on the autism spectrum (Level 1).",
    b: "The user prefers monorepos over polyrepos.",
  },
  {
    a: "The user grew up in a military family, moving often.",
    b: "The user uses Raycast as their launcher.",
  },
  {
    a: "The user has a pacemaker since 2021.",
    b: "The user prefers the Arc browser over Chrome.",
  },
  {
    a: "The user runs a podcast about distributed systems.",
    b: "The user has an egg allergy.",
  },
  {
    a: "The user lives with two roommates in a shared house.",
    b: "The user prefers Helix editor over Vim.",
  },
  {
    a: "The user has been freelancing for the past four years.",
    b: "The user is allergic to cats.",
  },
  {
    a: "The user is colorblind (red-green deuteranopia).",
    b: "The user prefers Bun over Node for new projects.",
  },
  {
    a: "The user is bilingual French-English from birth.",
    b: "The user uses 1Password for credential storage.",
  },
  {
    a: "The user just completed a coding bootcamp.",
    b: "The user prefers Caddy over nginx as a reverse proxy.",
  },
  {
    a: "The user is a professional opera singer.",
    b: "The user uses fish shell instead of zsh.",
  },
  {
    a: "The user has narcolepsy controlled with medication.",
    b: "The user prefers Kotlin over Java for Android.",
  },
  {
    a: "The user is married to another software engineer.",
    b: "The user uses Logseq for daily journaling.",
  },
  {
    a: "The user grew up on a sheep farm in New Zealand.",
    b: "The user prefers Vitest over Jest.",
  },
  {
    a: "The user has Tourette syndrome (mild).",
    b: "The user prefers Cursor over GitHub Copilot.",
  },
  {
    a: "The user runs an annual board-game convention.",
    b: "The user has a tree-nut allergy.",
  },
  {
    a: "The user uses a wheelchair full-time.",
    b: "The user prefers Pulumi over Terraform.",
  },
  {
    a: "The user is a Quaker by religious tradition.",
    b: "The user uses Cloudflare Workers for edge functions.",
  },
  {
    a: "The user retired from professional baseball at 30.",
    b: "The user prefers SQLite over Postgres for prototypes.",
  },
  {
    a: "The user grew up in foster care.",
    b: "The user uses Plausible Analytics over Google Analytics.",
  },
  {
    a: "The user just emigrated from Argentina last year.",
    b: "The user has fructose malabsorption.",
  },
  {
    a: "The user is a former Olympic figure skater.",
    b: "The user prefers ESLint flat config over the legacy format.",
  },
  {
    a: "The user owns a small vineyard inherited from family.",
    b: "The user uses Sentry for error tracking.",
  },
  {
    a: "The user is left-handed and writes only with fountain pens.",
    b: "The user prefers Astro over Next.js for content sites.",
  },
];

/**
 * 50 pairs. Same fact, paraphrased two ways. These are what `remember()`
 * SHOULD dedup. Active/passive swap, synonym substitution, word reorder.
 */
export const trueDuplicate: Pair[] = [
  {
    a: "The user works at Acme as a senior engineer.",
    b: "The user is employed by Acme as a senior engineer.",
  },
  {
    a: "The user prefers Neovim as their editor.",
    b: "Neovim is the user's editor of choice.",
  },
  { a: "The user has two children.", b: "The user is the parent of two kids." },
  {
    a: "The user holds a PhD in computer science.",
    b: "The user has a doctorate in computer science.",
  },
  {
    a: "The user is allergic to shellfish.",
    b: "The user has a shellfish allergy.",
  },
  { a: "The user lives in Brooklyn.", b: "The user resides in Brooklyn." },
  {
    a: "The user runs marathons twice a year.",
    b: "The user completes two marathons annually.",
  },
  {
    a: "The user prefers TypeScript over JavaScript.",
    b: "The user favors TypeScript over plain JavaScript.",
  },
  { a: "The user is fluent in Spanish.", b: "The user speaks fluent Spanish." },
  {
    a: "The user uses Linear for project tracking.",
    b: "The user tracks projects in Linear.",
  },
  {
    a: "The user grew up in rural Vermont.",
    b: "The user was raised in rural Vermont.",
  },
  {
    a: "The user is currently writing a novel.",
    b: "The user is in the process of writing a novel.",
  },
  {
    a: "The user works remotely from a home office.",
    b: "The user works from home in a home office.",
  },
  {
    a: "The user prefers PostgreSQL over MySQL.",
    b: "The user favors PostgreSQL over MySQL.",
  },
  {
    a: "The user is training for a triathlon.",
    b: "The user is preparing for a triathlon.",
  },
  {
    a: "The user is a licensed private pilot.",
    b: "The user holds a private pilot's license.",
  },
  {
    a: "The user prefers tea over coffee.",
    b: "The user prefers drinking tea to coffee.",
  },
  {
    a: "The user lived in Tokyo for three years.",
    b: "The user spent three years living in Tokyo.",
  },
  {
    a: "The user is gluten-intolerant.",
    b: "The user cannot tolerate gluten.",
  },
  {
    a: "The user volunteers at an animal shelter.",
    b: "The user does volunteer work at an animal shelter.",
  },
  {
    a: "The user codes primarily in Rust and Go.",
    b: "The user's main programming languages are Rust and Go.",
  },
  {
    a: "The user holds an MBA from Wharton.",
    b: "The user earned an MBA at Wharton.",
  },
  {
    a: "The user collects vintage typewriters.",
    b: "The user is a collector of vintage typewriters.",
  },
  {
    a: "The user prefers analog film photography.",
    b: "The user favors analog film for photography.",
  },
  {
    a: "The user attended Stanford for undergrad.",
    b: "The user did their undergraduate studies at Stanford.",
  },
  {
    a: "The user uses tmux for terminal multiplexing.",
    b: "The user runs tmux as their terminal multiplexer.",
  },
  {
    a: "The user owns a Tesla Model 3.",
    b: "The user drives a Tesla Model 3.",
  },
  {
    a: "The user has been sober for four years.",
    b: "The user has not consumed alcohol in four years.",
  },
  {
    a: "The user speaks intermediate Mandarin.",
    b: "The user has intermediate Mandarin proficiency.",
  },
  {
    a: "The user is a certified scuba diver.",
    b: "The user holds a scuba diving certification.",
  },
  {
    a: "The user uses GitHub Copilot daily.",
    b: "The user makes daily use of GitHub Copilot.",
  },
  { a: "The user has type-1 diabetes.", b: "The user is a type-1 diabetic." },
  {
    a: "The user is a fan of the Boston Celtics.",
    b: "The user supports the Boston Celtics.",
  },
  {
    a: "The user works at a Series B fintech startup.",
    b: "The user is employed at a Series B fintech startup.",
  },
  {
    a: "The user uses Obsidian for knowledge management.",
    b: "The user manages their knowledge in Obsidian.",
  },
  {
    a: "The user is on a sabbatical year.",
    b: "The user is taking a year-long sabbatical.",
  },
  {
    a: "The user is a member of a rock-climbing gym.",
    b: "The user holds a membership at a rock-climbing gym.",
  },
  {
    a: "The user maintains a popular open-source project.",
    b: "The user is the maintainer of a well-known open-source project.",
  },
  {
    a: "The user is engaged to be married next September.",
    b: "The user's wedding is scheduled for next September.",
  },
  {
    a: "The user uses pnpm for Node projects.",
    b: "The user's package manager for Node projects is pnpm.",
  },
  { a: "The user keeps kosher.", b: "The user follows a kosher diet." },
  {
    a: "The user took up oil painting last year.",
    b: "The user started oil painting one year ago.",
  },
  {
    a: "The user prefers AWS over GCP.",
    b: "The user favors AWS over Google Cloud.",
  },
  { a: "The user is an only child.", b: "The user has no siblings." },
  {
    a: "The user is allergic to penicillin.",
    b: "The user has a penicillin allergy.",
  },
  {
    a: "The user worked at Google for six years.",
    b: "The user spent six years at Google.",
  },
  {
    a: "The user prefers Notion to Confluence.",
    b: "The user favors Notion over Confluence.",
  },
  {
    a: "The user once interned at NASA.",
    b: "The user did an internship at NASA.",
  },
  {
    a: "The user holds a CCNA certification.",
    b: "The user is CCNA certified.",
  },
  {
    a: "The user serves on the board of a non-profit.",
    b: "The user is a board member of a non-profit.",
  },
];

/**
 * 30 pairs. Same shape and topic, different specific value.
 * `remember()` MUST NOT dedup these — they are distinct facts.
 * The dangerous band: cosine often inflates these toward the
 * paraphrase region because surface form is near-identical.
 */
export const relatedDistinct: Pair[] = [
  { a: "The user works at Acme.", b: "The user works at Initech." },
  {
    a: "The user prefers Adobe Premiere Pro.",
    b: "The user prefers Final Cut Pro.",
  },
  {
    a: "The user is allergic to shellfish.",
    b: "The user is allergic to peanuts.",
  },
  { a: "The user lives in Brooklyn.", b: "The user lives in Queens." },
  {
    a: "The user holds a PhD in computer science.",
    b: "The user holds a PhD in physics.",
  },
  { a: "The user has two children.", b: "The user has three children." },
  {
    a: "The user prefers TypeScript over JavaScript.",
    b: "The user prefers Python over JavaScript.",
  },
  {
    a: "The user runs marathons twice a year.",
    b: "The user runs marathons four times a year.",
  },
  { a: "The user is fluent in Spanish.", b: "The user is fluent in French." },
  {
    a: "The user attended Stanford for undergrad.",
    b: "The user attended MIT for undergrad.",
  },
  {
    a: "The user prefers PostgreSQL over MySQL.",
    b: "The user prefers MySQL over PostgreSQL.",
  },
  {
    a: "The user works remotely from a home office.",
    b: "The user works in-office five days a week.",
  },
  { a: "The user owns a Tesla Model 3.", b: "The user owns a Tesla Model Y." },
  {
    a: "The user has been sober for four years.",
    b: "The user has been sober for ten years.",
  },
  {
    a: "The user codes primarily in Rust.",
    b: "The user codes primarily in Go.",
  },
  { a: "The user has type-1 diabetes.", b: "The user has type-2 diabetes." },
  {
    a: "The user is a fan of the Boston Celtics.",
    b: "The user is a fan of the Los Angeles Lakers.",
  },
  { a: "The user prefers AWS over GCP.", b: "The user prefers GCP over AWS." },
  {
    a: "The user worked at Google for six years.",
    b: "The user worked at Google for two years.",
  },
  {
    a: "The user uses Obsidian for knowledge management.",
    b: "The user uses Notion for knowledge management.",
  },
  {
    a: "The user follows a vegetarian diet.",
    b: "The user follows a vegan diet.",
  },
  {
    a: "The user is married to a doctor.",
    b: "The user is married to a teacher.",
  },
  {
    a: "The user holds an MBA from Wharton.",
    b: "The user holds an MBA from Stanford GSB.",
  },
  {
    a: "The user prefers Vim keybindings.",
    b: "The user prefers Emacs keybindings.",
  },
  {
    a: "The user is training for a triathlon.",
    b: "The user is training for an Ironman.",
  },
  {
    a: "The user grew up in rural Vermont.",
    b: "The user grew up in rural Maine.",
  },
  {
    a: "The user uses Linear for project tracking.",
    b: "The user uses Jira for project tracking.",
  },
  {
    a: "The user is allergic to penicillin.",
    b: "The user is allergic to ibuprofen.",
  },
  {
    a: "The user lives in a fourth-floor walkup.",
    b: "The user lives in a ground-floor brownstone.",
  },
  {
    a: "The user prefers TypeScript strict mode.",
    b: "The user prefers TypeScript loose mode.",
  },
];
