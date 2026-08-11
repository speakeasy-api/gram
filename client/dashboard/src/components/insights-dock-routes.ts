/**
 * True for routes whose page mounts its OWN RemoteThreadListRuntime — the
 * Playground, Chat Elements, and the assistant onboarding editor
 * (/assistants/new and /assistants/:id). The shared Project Assistant runtime
 * must NOT be mounted above these pages: assistant-ui throws when one
 * RemoteThreadListRuntime is nested inside another. This is a synchronous,
 * render-time signal (keyed off the route) rather than the ref-counted
 * dock-hide, which flips in a layout effect — too late, since the nested inner
 * runtime already threw during the child's render. The bare `/assistants` list
 * has no runtime of its own and still wants the docked composer, so only its
 * sub-routes match.
 *
 * Lives in its own module (not `insights-dock.tsx`) so it can be unit-tested
 * without pulling in that component's heavy import graph.
 */
export function pageHostsOwnAssistantRuntime(pathname: string): boolean {
  return (
    /\/playground(\/|$)/.test(pathname) ||
    /\/elements(\/|$)/.test(pathname) ||
    /\/assistants\/[^/]/.test(pathname)
  );
}
