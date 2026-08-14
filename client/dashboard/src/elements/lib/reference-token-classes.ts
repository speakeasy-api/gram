import type { ComposerSegmentKind } from "@/elements/lib/tool-mentions";

/**
 * How an `@tool` / `/skill` token is painted, per surface.
 *
 * `surface` is the composer, where a token is a real `contenteditable={false}`
 * inline element — so it pads like a chip and the browser walks the caret
 * around it. (It could not, back when the composer was a textarea with the
 * draft mirrored underneath: a painted chip that occupied width slid the caret
 * off its glyphs.)
 *
 * The user bubble uses the same set: it is a bordered card on the page's own
 * background, so a chip needs no inverted variant to stay readable in either
 * theme.
 */
export const REFERENCE_TOKEN_CLASSES: Record<
  "surface",
  Record<ComposerSegmentKind, string>
> = {
  surface: {
    text: "",
    tool: "rounded-[4px] bg-mention-muted px-1.5 py-0.5 text-mention",
    skill: "rounded-[4px] bg-skill-muted px-1.5 py-0.5 text-skill",
  },
};
