/**
 * CenteredPage — the shell for full-viewport, standalone pages rendered outside
 * the main app layout: auth (login/register/signup), demo redirects, access
 * requests, not-found, policy acknowledgements.
 *
 * These already share two shells; this names them as the canonical choice so a
 * new standalone page reaches for one of these rather than hand-rolling a
 * centered `<main>`:
 *  - `CenteredPage` (= FullScreenPage): chrome-less, centered under the
 *    Speakeasy mark, with a "back to home" escape hatch.
 *  - `AuthShell`: the login/signup surface.
 */
export { FullScreenPage as CenteredPage } from "@/components/full-screen-page";
export { AuthShell } from "@/pages/login/components/auth-shell";
