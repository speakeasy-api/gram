---
"dashboard": patch
---

Render `@tool` and `/skill` references in the assistant composer as colored
chips, and make the composer a contenteditable so they can be real inline
elements rather than paint under a transparent textarea.

A textarea holds one flat string, so a reference inside a draft could only be
mirrored underneath the input — and anything painted there that occupied width
slid the caret off the glyphs after it. The input is now a `plaintext-only`
contenteditable whose chips are `contentEditable={false}` spans with real
padding, so the browser places the caret around them. The draft still lives on
the runtime as a string: the element reports edits as text and exposes
`value` / `selectionStart` / `selectionEnd` / `setSelectionRange`, so the
mention autocomplete, prompt recall, and dictation are unchanged.

Skills move into the draft with them. Picking a skill writes its `/name` token
into the text (and focuses the input, caret at the end) instead of only
toggling hidden state, and the composer derives the attached-skill set back out
of the draft — so deleting the token detaches the skill, and a message carrying
nothing but a reference can be sent. Tool names containing hyphens now match at
all; hyphenated source-slug names previously stopped at the first hyphen and
resolved to nothing. The badge rows above the input are gone, since the tokens
name themselves, and sent messages render the same chips in a bordered
white bubble.
