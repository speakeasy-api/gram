// Placeholders in index.html that the dashboard image's entrypoint
// (41-preview-runtime-values.sh) substitutes at container start. Keep this
// list in step with the envsubst variable list in that script.
const PREVIEW_RUNTIME_PLACEHOLDERS = [
  "${GRAM_GUTTERNOTE_SCRIPT}",
  "${GRAM_BRANCH}",
  "${GRAM_COMMIT_SHA}",
  "${GRAM_PR_NUMBER}",
];

/**
 * Blanks the preview placeholders for the dev server, which has no entrypoint
 * to substitute them. `${GRAM_GUTTERNOTE_SCRIPT}` sits in the body, so left
 * alone it renders as visible text on every locally served page; the others
 * sit in meta attributes and are merely wrong rather than visible.
 *
 * Dev only — the production build must emit the placeholders untouched.
 */
export function blankPreviewRuntimeValues(html: string): string {
  return PREVIEW_RUNTIME_PLACEHOLDERS.reduce(
    (acc, placeholder) => acc.replaceAll(placeholder, ""),
    html,
  );
}
