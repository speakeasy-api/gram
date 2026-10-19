# UI states in code

How to implement the critique checklist in `SKILL.md`. Each section maps to checklist lines.

## Technical detail behind disclosure

Put protocol fields, endpoints, slugs, and raw values in a `Collapsible` from `@/components/ui/Collapsible`, closed by default, with a trigger labelled "Technical details" or "Advanced".

## Errors

- Raw `error.message` / server text is never the primary message — not a heading, badge, inline error, or `toast.error(err.message)`. It may appear only as secondary detail under a plain-words title (as `handleError` renders it). Map each known error to plain words plus a next step; map unknown values to a safe default, never the raw string.
- Toast failures with `handleError(error, { title: "Couldn't delete the key" })` from `@/lib/errors`. The title carries the plain-words message; the server text sits under it as secondary detail. Do not use `handleAPIError` — its friendly message almost never shows.
- Show a specific refusal the user must act on inline, not in a toast. In a `ConfirmDialog`, pass it as `error` (a node, so it can link to the fix). Branch with `error instanceof GramError && error.statusCode === 409` (`GramError` from `@gram/client/models/errors/gramerror.js`).
- If the next action needs an API that does not exist, do not drop it silently: show the button disabled with a one-line reason, list the gap in your reply and the PR, and add the endpoint with the `gram-management-api` skill if it is in scope. Problems the user can only wait out (a rate limit) get a quiet status, no button.

## IDs and dates

- IDs and slugs: a muted, copyable second line under the name, or on the detail view. Never a title or a column of their own.
- Dates: `HumanizeDateTime` or `formatRelativeTime` from `@/lib/dates`. Never ISO strings.

## Loading, empty, and error without layout shift

- Pickers, bars, and query-backed cards have fixed heights. Skeletons match the loaded geometry line for line (`SkeletonTable`, geometry-matched rows).
- Never `return null` while a query loads or errors — the card pops in later. Render a skeleton, then content or an inline error with **Retry** (`InlineEmptyState` with `icon="triangle-alert"` and a Retry `action`).
- A filtered table keeps its height and shows its empty state inside it.
- Never key a data component on filter or search values (``key={`${filter}:${search}`}``); it remounts and flashes the skeleton. Keep it mounted with `placeholderData: keepPreviousData`, show the skeleton on first load only, and reset only the page index (`usePagedRows` `resetOn`, see `tables.md`).
- With a page template, compute `isEmpty` from the unfiltered rows and exclude errors (`!q.isError && rows.length === 0`); `isEmpty` hides the toolbar.

## Long values

- Names beside badges: `min-w-0` + `truncate`, full value in a tooltip.
- Hand-rolled grid tracks: `minmax(0,1fr)`. `Table` cells already clip, so give `Table` columns `fr`/`px` widths.
- Badge rows wrap (`flex-wrap`).
- Test with the longest real value in the seed data.

## Sizing

- Size columns with `Table` column `width`; let the name column take `1fr`; right-align numbers.
- No arbitrary pixel classes for layout (`pl-[52px]`, `w-[560px]`, `min-w-[1040px]`). Use spacing tokens and component props.
- Interactive surfaces (drag, resize, click) need hover, active, and focus states built from tokens, not raw `rgb()` or shadows.
