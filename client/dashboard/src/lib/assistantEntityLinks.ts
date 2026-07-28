import type { LinkResolver, ResolvedLink } from "@/elements";
import { useCallback } from "react";

import { useProjectSlugForRequests, useSlugs } from "@/contexts/Sdk";

/**
 * Scheme the Project Assistant emits to reference an in-app entity, e.g.
 * `gram:risk_policy/<id>` or `gram:chat/<chat-id>`. Elements stays agnostic to
 * this scheme; the resolver below (and the assistant's system prompt) own it.
 */
const SCHEME = "gram:";

/** An internal reference we recognise but can't turn into a route right now. */
const UNRESOLVABLE: ResolvedLink = { href: null };

function newTab(href: string): ResolvedLink {
  return { href, target: "_blank", rel: "noopener noreferrer" };
}

const enc = encodeURIComponent;

/**
 * Builds a dashboard URL for a `gram:` entity reference. Returns `null` for a
 * non-`gram:` href (leave it as an ordinary link), or `{ href: null }` for a
 * recognised-but-unresolvable reference (render as plain text — no dead link).
 *
 * The scheme is `gram:<type>/<id>[/<id2>]`. `type` is the first segment; the
 * remainder is the entity id (kept verbatim, so ids may contain `/`), except
 * for the two-id types (`source`, `remote_session_client`) which split once.
 */
function resolveEntityLink(
  href: string,
  orgSlug: string | undefined,
  projectSlug: string | undefined,
): ResolvedLink | null {
  if (!href.startsWith(SCHEME)) return null;

  const raw = href.slice(SCHEME.length);
  const sep = raw.indexOf("/");
  const type = sep === -1 ? raw : raw.slice(0, sep);
  const rest = sep === -1 ? "" : raw.slice(sep + 1);

  if (!orgSlug || !rest) return UNRESOLVABLE;

  const org = `/${orgSlug}`;
  // Project-scoped routes need a project slug; fall back to the request-scoped
  // project (honours ?projectSlug=, else the org's `default`) when the dock is
  // open on an org-only page.
  const proj = projectSlug ? `${org}/projects/${projectSlug}` : undefined;

  // Two-id types: split the remainder once.
  const slash = rest.indexOf("/");
  const id1 = slash === -1 ? rest : rest.slice(0, slash);
  const id2 = slash === -1 ? "" : rest.slice(slash + 1);

  switch (type) {
    // --- Top-level (no org/project prefix) ---
    case "block":
      return newTab(`/blocks/${enc(rest)}`);

    // --- Org-scoped ---
    case "collection":
      return newTab(`${org}/collections/${enc(rest)}`);
    case "remote_idp":
      return newTab(`${org}/remote-identity-providers/${enc(rest)}`);
    case "remote_session_client":
      return id2
        ? newTab(
            `${org}/remote-identity-providers/${enc(id1)}/clients/${enc(id2)}`,
          )
        : UNRESOLVABLE;
    case "user_session":
      // No per-session detail route — link to the connections list.
      return newTab(`${org}/user-sessions`);
  }

  // --- Project-scoped ---
  if (!proj) return UNRESOLVABLE;
  switch (type) {
    case "mcp_server":
      return newTab(`${proj}/mcp/x/${enc(rest)}`);
    case "toolset":
      return newTab(`${proj}/mcp/${enc(rest)}`);
    case "deployment":
      return newTab(`${proj}/deployments/${enc(rest)}`);
    case "chat":
    case "agent_session":
      return newTab(`${proj}/chat/${enc(rest)}`);
    case "risk_policy":
      return newTab(`${proj}/risk-policies?policy=${enc(rest)}`);
    case "risk_user":
      return newTab(`${proj}/risk-overview/users/${enc(rest)}`);
    case "risk_category":
      return newTab(`${proj}/risk-overview/categories/${enc(rest)}`);
    case "employee":
      return newTab(`${proj}/employees/${enc(rest)}`);
    case "environment":
      return newTab(`${proj}/environments/${enc(rest)}`);
    case "plugin":
      return newTab(`${proj}/plugins/${enc(rest)}`);
    case "prompt":
      return newTab(`${proj}/prompts/${enc(rest)}`);
    case "custom_tool":
      return newTab(`${proj}/custom-tools/${enc(rest)}`);
    case "catalog":
      return newTab(`${proj}/catalog/${enc(rest)}`);
    case "assistant":
      return newTab(`${proj}/assistants/${enc(rest)}`);
    case "source":
      return id2
        ? newTab(`${proj}/sources/${enc(id1)}/${enc(id2)}`)
        : UNRESOLVABLE;
    default:
      // A recognised-looking `gram:` ref of an unknown type: drop it rather
      // than emit a broken link.
      return UNRESOLVABLE;
  }
}

/**
 * Returns a {@link LinkResolver} that maps the Project Assistant's `gram:`
 * entity references to real dashboard routes, scoped to the current org/project.
 */
export function useAssistantLinkResolver(): LinkResolver {
  const { orgSlug, projectSlug } = useSlugs();
  const requestProjectSlug = useProjectSlugForRequests();
  const effectiveProject = projectSlug ?? requestProjectSlug;

  return useCallback(
    (href: string) => resolveEntityLink(href, orgSlug, effectiveProject),
    [orgSlug, effectiveProject],
  );
}
