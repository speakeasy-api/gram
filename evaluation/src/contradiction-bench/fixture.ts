/**
 * Contradiction-detection fixture: ~190 labelled pairs across four buckets.
 *
 * The probe asks whether memory B contradicts memory A. In the proposed
 * three-branch write logic, only `contradicts: true` triggers Supersede;
 * the other three buckets all fall through to Create.
 *
 * Buckets:
 *   - contradicting: B asserts a different value for the same dimension as A
 *   - refining:      B is a more specific or updated form of A (same person, both true)
 *   - extending:     B is about a different dimension entirely (both true)
 *   - unrelated:     A and B are about different topics
 *
 * Refining + extending + unrelated all carry gold label `contradicts: false`.
 * The bucket diversity exists so per-model false-positive analysis can
 * tell us *which kind* of non-contradiction the model gets wrong.
 */

export type Pair = { a: string; b: string };

/**
 * 55 pairs. Same dimension, different value. B should Supersede A.
 * Includes the structurally hardest cases: ordering swaps, version bumps,
 * count updates, role transitions.
 */
export const contradicting: Pair[] = [
  {
    a: "The user prefers Vim as their editor.",
    b: "The user prefers Emacs as their editor.",
  },
  {
    a: "The user prefers PostgreSQL over MySQL.",
    b: "The user prefers MySQL over PostgreSQL.",
  },
  { a: "The user prefers AWS over GCP.", b: "The user prefers GCP over AWS." },
  {
    a: "The user works at Acme as a senior engineer.",
    b: "The user works at Initech as a senior engineer.",
  },
  { a: "The user lives in Brooklyn.", b: "The user lives in San Francisco." },
  { a: "The user has two children.", b: "The user has three children." },
  {
    a: "The user follows a vegetarian diet.",
    b: "The user follows a pescatarian diet.",
  },
  { a: "The user is single.", b: "The user is married." },
  {
    a: "The user runs marathons twice a year.",
    b: "The user runs marathons four times a year.",
  },
  {
    a: "The user uses Node.js version 18.",
    b: "The user uses Node.js version 22.",
  },
  { a: "The user has type-1 diabetes.", b: "The user has type-2 diabetes." },
  {
    a: "The user worked at Google for six years.",
    b: "The user worked at Google for two years.",
  },
  {
    a: "The user prefers TypeScript strict mode.",
    b: "The user prefers TypeScript loose mode.",
  },
  {
    a: "The user's personal best 5k time is 25:50.",
    b: "The user's personal best 5k time is 22:14.",
  },
  {
    a: "The user is a junior developer.",
    b: "The user is a senior staff engineer.",
  },
  {
    a: "The user uses Obsidian for knowledge management.",
    b: "The user uses Notion for knowledge management.",
  },
  { a: "The user prefers Postgres 14.", b: "The user prefers Postgres 16." },
  {
    a: "The user is the only child of two physicians.",
    b: "The user is the youngest of four siblings.",
  },
  {
    a: "The user attended Stanford for undergrad.",
    b: "The user attended MIT for undergrad.",
  },
  { a: "The user is left-handed.", b: "The user is right-handed." },
  {
    a: "The user is allergic to peanuts.",
    b: "The user is no longer allergic to peanuts after immunotherapy.",
  },
  {
    a: "The user lives in a fourth-floor walkup in Manhattan.",
    b: "The user lives in a single-family home in Queens.",
  },
  {
    a: "The user is currently sober.",
    b: "The user drinks socially on weekends.",
  },
  {
    a: "The user works remotely full-time.",
    b: "The user works in-office five days a week.",
  },
  {
    a: "The user drives a 2019 Subaru Outback.",
    b: "The user drives a 2024 Subaru Outback.",
  },
  {
    a: "The user has been at the company for 3 years.",
    b: "The user has been at the company for 5 years.",
  },
  {
    a: "The user's daughter is named Emma.",
    b: "The user's daughter is named Sophia.",
  },
  {
    a: "The user prefers tea over coffee.",
    b: "The user prefers coffee over tea.",
  },
  { a: "The user uses fish shell.", b: "The user uses zsh." },
  {
    a: "The user runs Linux on their workstation.",
    b: "The user runs macOS on their workstation.",
  },
  {
    a: "The user is currently on parental leave.",
    b: "The user has returned from parental leave to full-time work.",
  },
  {
    a: "The user owns a golden retriever named Max.",
    b: "The user owns a golden retriever named Cooper.",
  },
  { a: "The user is a pescatarian.", b: "The user is a strict vegan." },
  {
    a: "The user uses npm for Node projects.",
    b: "The user uses pnpm for Node projects.",
  },
  { a: "The user's commute is by car.", b: "The user's commute is by subway." },
  { a: "The user's manager is Priya.", b: "The user's manager is Marcus." },
  {
    a: "The user is enrolled in evening MBA classes.",
    b: "The user has graduated from the MBA program.",
  },
  { a: "The user owns one cat.", b: "The user owns three cats." },
  {
    a: "The user prefers Kotlin for Android development.",
    b: "The user prefers Java for Android development.",
  },
  {
    a: "The user is in the final round of interviews at Stripe.",
    b: "The user accepted an offer at Stripe.",
  },
  {
    a: "The user uses GitHub Copilot daily.",
    b: "The user has stopped using AI coding assistants.",
  },
  {
    a: "The user's preferred language is English.",
    b: "The user's preferred language is French.",
  },
  {
    a: "The user owns a small vineyard in Sonoma.",
    b: "The user sold the Sonoma vineyard last year.",
  },
  {
    a: "The user attends Sunday services regularly.",
    b: "The user identifies as agnostic and does not attend services.",
  },
  {
    a: "The user works at a Series B fintech startup.",
    b: "The user works at a publicly-traded fintech company.",
  },
  {
    a: "The user has a peanut allergy.",
    b: "The user has outgrown the peanut allergy.",
  },
  {
    a: "The user is currently writing a novel.",
    b: "The user has finished and published the novel.",
  },
  {
    a: "The user is fluent in Spanish.",
    b: "The user speaks only conversational Spanish.",
  },
  {
    a: "The user follows a low-FODMAP diet.",
    b: "The user follows a Mediterranean diet.",
  },
  { a: "The user pays $1,800 in rent.", b: "The user pays $2,400 in rent." },
  {
    a: "The user is a beginner climber.",
    b: "The user is an advanced sport climber leading 5.12s.",
  },
  { a: "The user is engaged.", b: "The user is married." },
  { a: "The user uses iPhone 13.", b: "The user uses iPhone 16 Pro." },
  {
    a: "The user holds a green card.",
    b: "The user is a naturalized U.S. citizen.",
  },
  { a: "The user smokes occasionally.", b: "The user has quit smoking." },
];

/**
 * 40 pairs. B is more specific or a tightening of A. Both are still true.
 * The hard part: surface forms can look like contradictions.
 */
export const refining: Pair[] = [
  {
    a: "The user is a developer.",
    b: "The user is a senior staff engineer at Acme.",
  },
  {
    a: "The user lives in NYC.",
    b: "The user lives in Brooklyn's Park Slope neighborhood.",
  },
  {
    a: "The user uses Postgres.",
    b: "The user uses Postgres 16 with the pgvector extension.",
  },
  {
    a: "The user has a degree.",
    b: "The user has a PhD in computer science from Stanford.",
  },
  { a: "The user owns a car.", b: "The user owns a 2019 Subaru Outback." },
  {
    a: "The user exercises regularly.",
    b: "The user runs 5 miles every Tuesday and Thursday morning.",
  },
  {
    a: "The user has nut allergies.",
    b: "The user is allergic to peanuts and tree nuts.",
  },
  {
    a: "The user works in tech.",
    b: "The user works at Acme on the payments-infrastructure team.",
  },
  {
    a: "The user is married.",
    b: "The user is married to Priya, who is a pediatrician.",
  },
  {
    a: "The user has children.",
    b: "The user has two children, ages 5 and 8.",
  },
  {
    a: "The user owns a dog.",
    b: "The user owns a golden retriever named Max.",
  },
  {
    a: "The user has a digital fitness tracker.",
    b: "The user wears an Apple Watch Ultra 2.",
  },
  {
    a: "The user codes in Rust.",
    b: "The user codes in Rust for systems work and async services.",
  },
  {
    a: "The user has a chronic condition.",
    b: "The user has type-1 diabetes diagnosed at age 12.",
  },
  {
    a: "The user lived abroad.",
    b: "The user lived in Tokyo for three years from 2018 to 2021.",
  },
  {
    a: "The user travels often.",
    b: "The user has visited 38 countries so far.",
  },
  {
    a: "The user uses an editor.",
    b: "The user uses Neovim with custom Lua config.",
  },
  {
    a: "The user volunteers.",
    b: "The user volunteers at a local animal shelter every other Saturday.",
  },
  {
    a: "The user maintains an open-source project.",
    b: "The user maintains the popular Rust crate `tower-http`.",
  },
  {
    a: "The user holds a private pilot's license.",
    b: "The user holds a private pilot's license with instrument rating.",
  },
  {
    a: "The user is on a sabbatical.",
    b: "The user is on a year-long sabbatical hiking the Pacific Crest Trail.",
  },
  {
    a: "The user has worked in academia.",
    b: "The user has worked in academia for 15 years as a tenured professor.",
  },
  {
    a: "The user is studying for an exam.",
    b: "The user is studying for the California bar exam.",
  },
  {
    a: "The user runs a podcast.",
    b: "The user runs a podcast about distributed systems engineering.",
  },
  {
    a: "The user owns motorcycles.",
    b: "The user owns a 1969 Triumph Bonneville.",
  },
  {
    a: "The user is recovering from surgery.",
    b: "The user is recovering from ACL reconstruction on the right knee.",
  },
  {
    a: "The user grew up speaking multiple languages.",
    b: "The user grew up speaking Tagalog at home and English at school.",
  },
  {
    a: "The user lives in a multi-unit building.",
    b: "The user lives in a fourth-floor walkup in Manhattan.",
  },
  {
    a: "The user keeps backyard animals.",
    b: "The user keeps a flock of six laying hens in the backyard.",
  },
  {
    a: "The user has dietary restrictions.",
    b: "The user keeps strict kosher and avoids shellfish and pork.",
  },
  {
    a: "The user uses an AI coding tool.",
    b: "The user uses Cursor with Claude Sonnet as the default model.",
  },
  {
    a: "The user collects something.",
    b: "The user collects vintage typewriters from the 1920s and 1930s.",
  },
  {
    a: "The user uses a terminal multiplexer.",
    b: "The user uses tmux with custom keybindings via `.tmux.conf`.",
  },
  {
    a: "The user is a competitive athlete.",
    b: "The user is a masters-category swimmer competing in the 50m freestyle.",
  },
  {
    a: "The user uses a launcher.",
    b: "The user uses Raycast with custom AI-prompt extensions.",
  },
  {
    a: "The user works on infrastructure.",
    b: "The user works on payment-processing infrastructure handling $2B/year.",
  },
  {
    a: "The user has a coding background.",
    b: "The user has 12 years of professional software engineering experience.",
  },
  {
    a: "The user uses a note-taking app.",
    b: "The user uses Obsidian with the Dataview plugin for daily journaling.",
  },
  {
    a: "The user has a long commute.",
    b: "The user's commute is a 40-minute ferry ride from Staten Island.",
  },
  {
    a: "The user just bought a property.",
    b: "The user just bought a fixer-upper Victorian in upstate New York.",
  },
];

/**
 * 45 pairs. B is about a different dimension. Both true, no overlap.
 * The hard part: shared subject words ("Tesla", "Brooklyn") can falsely
 * pull a model toward "contradicts".
 */
export const extending: Pair[] = [
  { a: "The user is fluent in Spanish.", b: "The user is fluent in Italian." },
  { a: "The user owns a Tesla Model 3.", b: "The user owns a Tesla Model Y." },
  {
    a: "The user trains in Brazilian jiu-jitsu twice weekly.",
    b: "The user trains in Muay Thai twice weekly.",
  },
  {
    a: "The user holds a PhD in computer science.",
    b: "The user holds a master's in computer science.",
  },
  {
    a: "The user owns a 2018 MacBook Pro.",
    b: "The user owns a 2024 MacBook Pro.",
  },
  { a: "The user lives in Brooklyn.", b: "The user has two children." },
  { a: "The user works at Acme.", b: "The user graduated from MIT." },
  {
    a: "The user prefers Postgres.",
    b: "The user uses Linear for project tracking.",
  },
  {
    a: "The user is allergic to shellfish.",
    b: "The user prefers tea over coffee.",
  },
  {
    a: "The user owns a Tesla Model 3.",
    b: "The user also owns a 1969 Triumph Bonneville motorcycle.",
  },
  {
    a: "The user runs marathons.",
    b: "The user holds a PhD in computer science.",
  },
  {
    a: "The user lives in NYC.",
    b: "The user maintains an open-source Rust crate.",
  },
  {
    a: "The user has a peanut allergy.",
    b: "The user has a shellfish allergy.",
  },
  {
    a: "The user has a daughter named Emma.",
    b: "The user has a son named Liam.",
  },
  { a: "The user owns a golden retriever.", b: "The user owns a tabby cat." },
  {
    a: "The user prefers Vim keybindings.",
    b: "The user uses tmux for terminal multiplexing.",
  },
  {
    a: "The user works in tech.",
    b: "The user volunteers at an animal shelter.",
  },
  {
    a: "The user is on parental leave.",
    b: "The user is taking online MBA classes during leave.",
  },
  {
    a: "The user lived in Tokyo for three years.",
    b: "The user is fluent in conversational Japanese.",
  },
  {
    a: "The user is married.",
    b: "The user is the only child of two physicians.",
  },
  {
    a: "The user codes in Rust.",
    b: "The user also writes occasional Python for data analysis.",
  },
  {
    a: "The user has worked at Google for six years.",
    b: "The user previously worked at Microsoft for two years.",
  },
  {
    a: "The user trains in BJJ.",
    b: "The user runs a podcast about distributed systems.",
  },
  {
    a: "The user is a Boston Celtics fan.",
    b: "The user follows European Premier League soccer.",
  },
  {
    a: "The user keeps backyard chickens.",
    b: "The user grows tomatoes and basil in raised beds.",
  },
  {
    a: "The user uses GitHub Copilot.",
    b: "The user uses 1Password for credential storage.",
  },
  {
    a: "The user prefers AWS over GCP.",
    b: "The user uses Cloudflare Workers for edge functions.",
  },
  {
    a: "The user is engaged to be married.",
    b: "The user just bought a fixer-upper in upstate New York.",
  },
  {
    a: "The user holds a CCNA certification.",
    b: "The user holds an AWS Solutions Architect certification.",
  },
  {
    a: "The user is left-handed.",
    b: "The user is colorblind (red-green deuteranopia).",
  },
  {
    a: "The user grew up in rural Vermont.",
    b: "The user attended Stanford for undergrad.",
  },
  {
    a: "The user has type-1 diabetes.",
    b: "The user runs marathons twice a year.",
  },
  {
    a: "The user owns a Tesla Model 3.",
    b: "The user prefers analog film photography.",
  },
  { a: "The user is a pescatarian.", b: "The user is allergic to penicillin." },
  {
    a: "The user works remotely.",
    b: "The user takes a daily 30-minute meditation break.",
  },
  {
    a: "The user lives in a fourth-floor walkup.",
    b: "The user keeps a small herb garden on the windowsill.",
  },
  {
    a: "The user attended Stanford.",
    b: "The user later earned an MBA from Wharton.",
  },
  {
    a: "The user is a black belt in taekwondo.",
    b: "The user plays in a community orchestra.",
  },
  {
    a: "The user uses Obsidian.",
    b: "The user uses Logseq for daily journaling.",
  },
  {
    a: "The user grew up speaking Tagalog at home.",
    b: "The user studied French in high school.",
  },
  {
    a: "The user owns a vineyard.",
    b: "The user owns a small Etsy shop selling pottery.",
  },
  {
    a: "The user is married to a doctor.",
    b: "The user works as a software engineer.",
  },
  {
    a: "The user has narcolepsy.",
    b: "The user is a competitive chess player rated 1900.",
  },
  {
    a: "The user runs an annual board-game convention.",
    b: "The user has a tree-nut allergy.",
  },
  {
    a: "The user is a former Olympic figure skater.",
    b: "The user now works as a tax attorney.",
  },
];

/**
 * 50 pairs. Different topics entirely. Sets baseline for "obviously not
 * a contradiction." Reused in shape from the dedup-calibration fixture
 * but trimmed and re-balanced for variety.
 */
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
    a: "The user took up oil painting last year.",
    b: "The user prefers AWS over GCP for cloud infrastructure.",
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
    a: "The user lives with a chronic migraine condition.",
    b: "The user prefers Sublime Text for quick edits.",
  },
  {
    a: "The user holds a CCNA certification.",
    b: "The user has visited 38 countries so far.",
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
    a: "The user has worked in academia for 15 years.",
    b: "The user prefers boutique hotels over chains.",
  },
  {
    a: "The user runs a popular newsletter on AI ethics.",
    b: "The user has lactose intolerance.",
  },
];
