import { useSdkClient } from "@/contexts/Sdk";
import { useEffect, useState } from "react";

export function useMcpSlugValidation(
  mcpSlug: string | undefined,
  currentSlug: string | undefined,
  customDomainId: string | undefined,
): string | null {
  const [slugError, setSlugError] = useState<string | null>(null);
  const client = useSdkClient();

  function validateMcpSlug(slug: string) {
    if (!slug) return "MCP Slug is required";
    if (slug.length > 40) return "Must be 40 characters or fewer";
    if (!/^[a-z0-9_-]+$/.test(slug))
      return "Lowercase letters, numbers, _ or - only";
    return null;
  }

  useEffect(() => {
    let stale = false;
    setSlugError(null);

    if (mcpSlug && mcpSlug !== currentSlug) {
      const validationError = validateMcpSlug(mcpSlug);
      if (validationError) {
        setSlugError(validationError);
        return;
      }
      void client.mcpEndpoints
        .checkSlugAvailability({ slug: mcpSlug, customDomainId })
        .then((available) => {
          if (!available && !stale) {
            setSlugError("This slug is already taken");
          }
        });
    }
    return () => {
      stale = true;
    };
  }, [mcpSlug, currentSlug, customDomainId, client.mcpEndpoints]);

  return slugError;
}
