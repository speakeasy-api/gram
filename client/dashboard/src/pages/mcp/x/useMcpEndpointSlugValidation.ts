import { useSdkClient } from "@/contexts/Sdk";
import { useQuery } from "@tanstack/react-query";
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

const DEBOUNCE_MS = 250;

export function useMcpEndpointSlugValidation(
  draftSlug: string,
  customDomainId: string | null,
  currentSlug?: string,
): string | null {
  const client = useSdkClient();

  // Debounce the slug input so we don't fire two RPCs per keystroke while the
  // user is typing. The format check below stays synchronous so obvious
  // mistakes surface immediately.
  const [debouncedSlug, setDebouncedSlug] = useState(draftSlug);
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSlug(draftSlug), DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [draftSlug]);

  // No error to show when the draft matches the persisted value — nothing has
  // changed, so we skip both format and availability checks.
  const formatError =
    draftSlug === currentSlug ? null : validateSlugFormat(draftSlug);

  const shouldCheck =
    formatError === null &&
    debouncedSlug !== "" &&
    debouncedSlug !== currentSlug &&
    debouncedSlug === draftSlug;

  const { data: available } = useQuery({
    queryKey: [
      "mcpEndpointSlugAvailability",
      debouncedSlug,
      customDomainId,
    ] as const,
    enabled: shouldCheck,
    // Note the inverted semantics across the two RPCs:
    // - toolsets.checkMCPSlugAvailability returns true when the slug is
    //   TAKEN (`EXISTS`).
    // - mcpEndpoints.checkSlugAvailability returns true when the slug is
    //   AVAILABLE (`NOT EXISTS`).
    // The wrapper normalises both to a single "available" boolean.
    queryFn: async () => {
      const [toolsetTaken, endpointAvailable] = await Promise.all([
        client.toolsets.checkMCPSlugAvailability({ slug: debouncedSlug }),
        client.mcpEndpoints.checkSlugAvailability({
          slug: debouncedSlug,
          customDomainId: customDomainId ?? undefined,
        }),
      ]);
      return !toolsetTaken && endpointAvailable;
    },
  });

  if (formatError) return formatError;
  if (shouldCheck && available === false) return "This slug is already taken";
  return null;
}

function validateSlugFormat(slug: string): string | null {
  if (!slug) return "Slug is required";
  if (slug.length > 128) return "Must be 128 characters or fewer";
  if (!/^[a-z0-9_-]+$/.test(slug))
    return "Lowercase letters, numbers, _ or - only";
  return null;
}
