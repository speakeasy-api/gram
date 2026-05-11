import { useSdkClient } from "@/contexts/Sdk";
import { useEffect, useState } from "react";

// useMcpEndpointSlugValidation validates a draft mcp_endpoints slug against
// (a) the slug format constraints (lowercase, alnum + dash/underscore, length)
// and (b) availability in the relevant uniqueness namespace.
//
// Availability is checked against BOTH `mcpEndpoints.checkSlugAvailability`
// (the canonical check for the mcp_endpoints table) AND
// `toolsets.checkMCPSlugAvailability` (which spans the legacy toolsets table).
// The dual check is intentional and temporary while toolsets-backed MCP
// servers continue to live under `/mcp/<mcpSlug>` and Remote-MCP-backed
// servers live under `/x/mcp/<slug>` — until the AGE-1902 / AGE-1880 cutover
// consolidates both onto mcp_servers/mcp_endpoints, a slug taken by either
// namespace would collide at runtime if reused.
//
// Returns the latest validation error, or null when the draft is valid.
//
// TODO(AGE-1902): drop the toolsets.checkMCPSlugAvailability call once the
// toolset-backed runtime path migrates to mcp_endpoints — at that point the
// mcp_endpoints uniqueness index covers all slugs.
export function useMcpEndpointSlugValidation(
  draftSlug: string,
  customDomainId: string | null,
  currentSlug?: string,
): string | null {
  const [error, setError] = useState<string | null>(null);
  const client = useSdkClient();

  useEffect(() => {
    setError(null);

    if (draftSlug === currentSlug) return;

    const formatError = validateSlugFormat(draftSlug);
    if (formatError) {
      setError(formatError);
      return;
    }

    // Debounce the availability check so we don't fire two RPCs per keystroke
    // while the user is typing. The format check above stays synchronous so
    // obvious mistakes surface immediately.
    let cancelled = false;
    const timer = setTimeout(() => {
      Promise.all([
        // Note the inverted semantics across the two RPCs:
        // - toolsets.checkMCPSlugAvailability returns true when the slug is
        //   TAKEN (`EXISTS`).
        // - mcpEndpoints.checkSlugAvailability returns true when the slug is
        //   AVAILABLE (`NOT EXISTS`).
        // The wrapper normalises both to a single "available" boolean.
        client.toolsets
          .checkMCPSlugAvailability({ slug: draftSlug })
          .then((taken) => !taken),
        client.mcpEndpoints.checkSlugAvailability({
          slug: draftSlug,
          customDomainId: customDomainId ?? undefined,
        }),
      ])
        .then(([toolsetAvailable, endpointAvailable]) => {
          if (cancelled) return;
          if (!toolsetAvailable || !endpointAvailable) {
            setError("This slug is already taken");
          }
        })
        .catch(() => {
          // Network failures shouldn't block the user — the backend will
          // reject the eventual mutation if the slug really is taken.
        });
    }, 250);

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [
    draftSlug,
    customDomainId,
    currentSlug,
    client.toolsets,
    client.mcpEndpoints,
  ]);

  return error;
}

function validateSlugFormat(slug: string): string | null {
  if (!slug) return "Slug is required";
  if (slug.length > 128) return "Must be 128 characters or fewer";
  if (!/^[a-z0-9_-]+$/.test(slug))
    return "Lowercase letters, numbers, _ or - only";
  return null;
}
