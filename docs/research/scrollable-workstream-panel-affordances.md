# Scrollable workstream panel affordances

Research date: 2026-09-09.

## Scope and method

The target dashboard has four independently scrollable, bounded workstream panels. Each panel initially shows its first card in full. Later cards can fall below the visible panel edge. A scroll affordance is a cue that content can move.

This note follows the existing `docs/research/` convention: a Markdown investigation with direct citations and a separate recommendation. Sources are limited to NN/g original research, W3C/WAI specifications and guidance, and first-party design documentation. No secondary sources support this note.

I opened the cited pages and inspected their relevant text. Apple serves a JavaScript shell to the page reader. I inspected indexed first-party text from its June 2026 page variant and cite the canonical URL below. The source index includes that retrieval URL. The Material density page returned no usable text, so it supports no claims here.

No reviewed source directly compares these patterns in a dashboard with four independently scrollable workstream panels. Platform guidance, accessibility requirements, and observed usability findings are distinct kinds of evidence. Recommendations below are applications of that evidence, not measured results for this dashboard.

## Source-backed findings

### Partial next-item peek

NN/g describes an illusion of completeness: visible content appears to end despite additional content outside the view. Its study of a full-screen landing page found that six of eight participants did not discover scrolling. This is evidence about that page, not a failure rate for dashboard panels. [NN/g: The Illusion of Completeness](https://www.nngroup.com/articles/illusion-of-completeness/)

NN/g recommends exposing some subsequent content above the fold. It also identifies large gaps and strong horizontal breaks as possible false endings. [NN/g: The Illusion of Completeness](https://www.nngroup.com/articles/illusion-of-completeness/)

Apple explicitly recommends partial content at a scroll view edge to indicate more content in that direction. It explains that scroll indicators are not always visible. [Apple: Scroll views](https://developer.apple.com/design/human-interface-guidelines/scroll-views)

Evidence limit: these sources support the general peek pattern. They do not establish a minimum peek height, a card fraction, or a success rate for four columns.

### Persistent scrollbar

NN/g recommends standard platform scrollbars for areas with scrolling content and hiding scrollbars when everything fits. Its original testing found that users missed unconventional scrolling controls, sometimes restricting their choices to visible items. [NN/g: Scrolling and Scrollbars](https://www.nngroup.com/articles/scrolling-and-scrollbars/)

Microsoft documents scroll viewers that support touch, mouse, keyboard, and scrollbar interaction. Its indicator changes appearance with the input method, so its guidance does not prescribe a permanently expanded scrollbar. [Microsoft: Scroll viewer controls](https://learn.microsoft.com/en-us/windows/apps/develop/ui/controls/scroll-controls)

W3C leaves the choice of classic or overlay scrollbars to the browser. Overlay scrollbars occupy the content area without reserving a gutter. [W3C: CSS Overflow, scrollbar gutters](https://www.w3.org/TR/css-overflow-3/#scrollbar-gutter-property)

`scrollbar-gutter: stable` reserves space for classic scrollbars but does not change scrollbar visibility. It does not reserve space for overlay scrollbars. [W3C: CSS Overflow, scrollbar gutters](https://www.w3.org/TR/css-overflow-3/#scrollbar-gutter-property)

Evidence limit: standard visible scrollbars have direct support. A permanently visible custom scrollbar is not established as the best solution across platforms.

### Edge fade or gradient

Apple describes scroll edge effects as separation between floating interface elements and the content behind them. It limits their use to views behind floating elements and emphasizes control legibility. [Apple: Scroll views, Scroll edge effects](https://developer.apple.com/design/human-interface-guidelines/scroll-views#Scroll-edge-effects)

Evidence limit: this is guidance about separating layers, not evidence that a bottom gradient reveals hidden cards. No direct evidence for a standalone fade as a discovery cue emerged from the reviewed source set. Its use for this dashboard remains synthesis / unsupported.

### Sticky footer, count, or more cue

NN/g recommends visible arrows and slide counts for horizontal carousels. That context differs from a vertically scrolling workstream panel. [NN/g: The Illusion of Completeness](https://www.nngroup.com/articles/illusion-of-completeness/)

WAI identifies sticky headers and footers as possible obstructions to keyboard focus. WCAG 2.2 criterion 2.4.11, Level AA, prohibits author content from entirely hiding a component when it receives keyboard focus. [WAI: Focus Not Obscured (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/focus-not-obscured-minimum.html)

Evidence limit: the reviewed sources do not validate a sticky count or footer as a vertical overflow cue. A count of all cards and a count of cards below the current view are different proposed messages. Choosing either message and its location is synthesis.

### Explicit text or icon

NN/g presents a prompt to read beyond an interruption as a way to communicate continuation. Its research also records missed triangle controls whose scrolling function was unclear. [NN/g: The Illusion of Completeness](https://www.nngroup.com/articles/illusion-of-completeness/) [NN/g: Scrolling and Scrollbars](https://www.nngroup.com/articles/scrolling-and-scrollbars/)

Evidence limit: these findings support clear continuation cues and caution with unfamiliar controls. They do not prove that “Scroll for more” or a downward arrow works best in four panels. Exact wording, icon choice, and placement remain synthesis.

### Nested-scroll avoidance

Apple advises against placing a scroll view inside another scroll view with the same orientation because control becomes unpredictable. Microsoft recommends vertical scrolling where possible and using built-in scrolling behavior. [Apple: Scroll views](https://developer.apple.com/design/human-interface-guidelines/scroll-views) [Microsoft: Scroll viewer controls](https://learn.microsoft.com/en-us/windows/apps/develop/ui/controls/scroll-controls)

Evidence limit: four sibling panels are not themselves nested scroll views. The nesting issue applies when the surrounding page or a card also scrolls vertically. These sources do not prohibit four sibling panels or establish their comparative usability.

### Content density and expand-collapse

Fluent defines accordions as related sections that open and close. It advises against hiding information required for the current task inside an accordion. [Microsoft Fluent: Accordion](https://fluent2.microsoft.design/components/web/react/core/accordion/usage)

WAI defines a disclosure as a control that shows or hides content. Its pattern uses a button with `aria-expanded`, activated with Enter or Space. [WAI: Disclosure pattern](https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/)

Evidence limit: neither source establishes that denser cards improve scroll discovery. Reducing padding, shortening summaries, collapsing secondary details, or expanding a whole panel are design hypotheses for this dashboard.

### Accessibility, keyboard, and focus

WAI's scrollable-content test expects the scrolling element or a descendant to participate in sequential keyboard navigation. It warns that intercepted keyboard events require separate testing. This test alone does not establish full keyboard accessibility. [WAI: Scrollable content ACT rule](https://www.w3.org/WAI/standards-guidelines/act/rules/0ssw9k/)

WCAG requires a keyboard method to leave a component that accepts keyboard focus. Keyboard operation must also offer a visible focus indicator. [WAI: No Keyboard Trap](https://www.w3.org/WAI/WCAG22/Understanding/no-keyboard-trap.html) [WAI: Focus Visible](https://www.w3.org/WAI/WCAG22/Understanding/focus-visible.html)

A region landmark is a named section for assistive navigation. WAI requires a label for each region and recommends unique labels when several regions exist. Its example connects a section to a visible heading through `aria-labelledby`. [WAI: Region landmark](https://www.w3.org/WAI/ARIA/apg/patterns/landmarks/examples/region.html)

Normal cue text needs at least 4.5:1 contrast against its background. Large text has a 3:1 threshold, with other exceptions defined in the criterion. [WAI: Contrast (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html)

Visual information needed to identify author-styled controls or their states requires 3:1 contrast against adjacent colors. Unmodified browser controls are exempt from that requirement. [WAI: Non-text Contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html)

WAI warns that translucent or dimming overlays can reduce focus contrast even when a focused control is not entirely hidden. [WAI: Focus Not Obscured (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/focus-not-obscured-minimum.html)

WCAG reflow requires vertically scrolling content to work at 320 CSS pixels wide without information loss or two-dimensional scrolling. Exceptions apply to content that needs a two-dimensional layout for meaning or use. [WAI: Reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html)

### Reduced motion

WCAG criterion 2.3.3, Level AAA, requires a way to disable nonessential motion animation triggered by interaction. Essential movement under user control during ordinary scrolling is allowed. [WAI: Animation from Interactions](https://www.w3.org/WAI/WCAG22/Understanding/animation-from-interactions.html)

WAI technique C39 uses `prefers-reduced-motion` to suppress motion based on the user preference. This technique is a documented implementation route, not the only permitted route. [WAI: Technique C39](https://www.w3.org/WAI/WCAG22/Techniques/css/C39)

## Synthesis / recommendation

The following is a proposed design for the four panels, not a source-tested combination. Use a partial next-card peek plus native scrolling as the baseline. Add local text when the layout cannot expose the next card. Treat a gradient or sticky footer as an optional experiment.

### Baseline for all four panels

1. Keep each workstream heading visible above its scrolling card list.
2. Show a total beside each heading, such as “7 cards,” if that total is reliable.
3. Keep the first card fully visible at the intended desktop size.
4. Expose a recognizable part of the next card when another card exists.
5. Adjust excess card spacing or secondary content before shrinking text to create that peek.
6. Keep native scrollbars available within each panel.
7. Respect platform scrollbar visibility preferences.
8. If a peek is absent while content remains below, show “More cards below” beside that panel's heading.
9. Remove the directional cue when the panel reaches its end.

Do not force a fixed peek height across long titles, zoom levels, and variable card heights. The sources provide no validated pixel value. Use the text fallback when the first card consumes the available space. If the first card exceeds the panel height, preserve access to its full contents.

A heading count gives the proposed design a persistent statement of scope. It does not state where those cards are relative to the current view. Do not describe a total as “remaining” or “below.” Calculate any directional message separately for each panel, including after filtering, resizing, or expanding a card.

### Optional patterns and their limits

For an edge fade, test it only as a secondary cue. Show it only while content remains below. Keep readable card text and focus indicators outside the fade. Do not use Apple's floating-control effect as proof that this treatment improves discovery.

If testing a persistent bottom cue, reserve a separate footer row instead of covering the cards. Use “More cards below” rather than an unexplained count. Stop showing the cue at the end. Avoid a footer that removes the next-card peek to make room for itself.

If the cue performs an action, use a real button with an explicit label such as “Scroll to next card.” Make it scroll only its own panel. Treat that button as a separate candidate for testing. Do not style a passive hint as a button or rely on a lone chevron.

For dense cards, keep the workstream decision information visible in the collapsed summary. Put only secondary detail behind an explicit disclosure. Let the card grow within its panel when expanded. Avoid a second vertical scrollbar inside the card.

Keep the four panels as sibling scrolling regions at desktop sizes. Avoid adding another vertical scrolling layer around them where the layout permits. At narrow widths or high zoom, prefer stacked workstreams with page scrolling and unbounded lists. Do not assume that this dashboard qualifies for a reflow exception solely because it uses columns.

### Interaction requirements for the proposal

Give each panel a unique accessible name from its visible heading. Use a labeled region for each workstream, not for every card. Make each overflowing list keyboard reachable, with `tabindex="0"` where needed. Preserve ordinary browser scrolling keys instead of inventing column navigation shortcuts.

Keep focus visible as it enters cards below the fold. Allow Tab and Shift+Tab to leave every panel. If cards have many controls, test the effort required to reach the next workstream. Make sure that any footer or overlay leaves focused controls fully visible.

Use static discovery cues. Do not animate a bounce or briefly scroll all four panels to demonstrate overflow. If an action scrolls to another card, use an immediate transition when reduced motion is requested. Keep ordinary user-controlled scrolling available.

### Proposed evaluation

These are future evaluation steps, not tests performed during this research.

1. Ask users to find a later card in each workstream without mentioning scrolling.
2. Record whether users discover overflow and scroll the intended panel.
3. Compare the current view with the peek baseline and the text fallback.
4. Test empty, single-card, overflowing, and end-of-list states independently in all four panels.
5. Test long first cards, expansion, filtering, and window resizing.
6. Test mouse, trackpad, touch, keyboard, and screen-reader navigation.
7. Test hidden and always-visible system scrollbars, high contrast, reduced motion, and reflow at 320 CSS pixels.

## Source index

The links below are direct primary sources. NN/g pages report original observations and design guidance. Platform documents supply conventions, while W3C/WAI documents supply specifications, accessibility explanations, patterns, and techniques.

1. [NN/g: The Illusion of Completeness](https://www.nngroup.com/articles/illusion-of-completeness/)
2. [NN/g: Scrolling and Scrollbars](https://www.nngroup.com/articles/scrolling-and-scrollbars/)
3. [Apple: Scroll views](https://developer.apple.com/design/human-interface-guidelines/scroll-views), including [the indexed June 2026 retrieval variant](https://developer.apple.com/design/human-interface-guidelines/scroll-views?changes=_2).
4. [Microsoft: Scroll viewer controls](https://learn.microsoft.com/en-us/windows/apps/develop/ui/controls/scroll-controls)
5. [Microsoft Fluent: Accordion](https://fluent2.microsoft.design/components/web/react/core/accordion/usage)
6. [W3C: CSS Overflow Module Level 3, scrollbar gutters](https://www.w3.org/TR/css-overflow-3/#scrollbar-gutter-property)
7. [WAI: Disclosure pattern](https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/)
8. [WAI: Scrollable content ACT rule](https://www.w3.org/WAI/standards-guidelines/act/rules/0ssw9k/)
9. [WAI: No Keyboard Trap](https://www.w3.org/WAI/WCAG22/Understanding/no-keyboard-trap.html)
10. [WAI: Focus Visible](https://www.w3.org/WAI/WCAG22/Understanding/focus-visible.html)
11. [WAI: Focus Not Obscured (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/focus-not-obscured-minimum.html)
12. [WAI: Region landmark](https://www.w3.org/WAI/ARIA/apg/patterns/landmarks/examples/region.html)
13. [WAI: Contrast (Minimum)](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html)
14. [WAI: Non-text Contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html)
15. [WAI: Reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html)
16. [WAI: Animation from Interactions](https://www.w3.org/WAI/WCAG22/Understanding/animation-from-interactions.html)
17. [WAI: Technique C39](https://www.w3.org/WAI/WCAG22/Techniques/css/C39)
