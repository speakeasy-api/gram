import { useSdkClient } from "@/contexts/Sdk";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";

// useMcpEndpointSlugValidation validates a draft mcp_endpoints slug against
// (a) the slug format constraints (lowercase, alnum + dash/underscore, length)
// and (b) availability in the relevant uniqueness namespace.
//
// Availability is checked with `mcpEndpoints.checkSlugAvailability`, which
// spans both tables that can hold a live MCP address (mcp_endpoints.slug and
// the legacy toolsets.mcp_slug) in one unified namespace per address scope.
//
// Returns the latest validation error, or null when the draft is valid.

const DEBOUNCE_MS = 250;
const PLATFORM_SLUG_MAX_LENGTH = 40;
const CUSTOM_DOMAIN_SLUG_MAX_LENGTH = 128;

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
  let formatError: string | null = null;
  if (draftSlug !== currentSlug) {
    formatError = validateSlugFormat(draftSlug, customDomainId);
  }

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
    queryFn: () =>
      client.mcpEndpoints.checkSlugAvailability({
        slug: debouncedSlug,
        customDomainId: customDomainId ?? undefined,
      }),
  });

  if (formatError) return formatError;
  if (shouldCheck && available === false) return "This slug is already taken";
  return null;
}

function validateSlugFormat(
  slug: string,
  customDomainId: string | null,
): string | null {
  let maxLength = PLATFORM_SLUG_MAX_LENGTH;
  if (customDomainId) {
    maxLength = CUSTOM_DOMAIN_SLUG_MAX_LENGTH;
  }

  if (!slug) return "Slug is required";
  if (slug.length > maxLength)
    return `Must be ${maxLength} characters or fewer`;
  if (!/^[a-z0-9_-]+$/.test(slug))
    return "Lowercase letters, numbers, _ or - only";
  return null;
}
