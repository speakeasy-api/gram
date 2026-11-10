import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLinkIcon } from "lucide-react";
import { adminGetMcpVerificationLink } from "@/lib/gramAdminClient";
import { Button } from "@/components/ui/button";

/**
 * Hands a traced server to the customer dashboard, where staff sign in to it
 * and run its tools in the Playground. The link only reads: when Speakeasy's
 * project already has the server it opens the Playground, otherwise the
 * add-server page with the URL filled in, where staff confirm the addition.
 */
export function VerifyInSpeakeasy({
  url,
  name,
}: {
  url: string;
  name?: string;
}): JSX.Element {
  const link = useQuery({
    queryKey: ["admin", "mcp-verification-link", url, name ?? ""],
    queryFn: () => adminGetMcpVerificationLink(url, name),
    retry: false,
    staleTime: 30_000,
  });

  if (link.error) {
    return (
      <span className="text-muted-foreground text-xs" role="status">
        Verify in Speakeasy is unavailable: {link.error.message}
      </span>
    );
  }
  if (!link.data) {
    return (
      <Button size="sm" disabled>
        Verify in Speakeasy
      </Button>
    );
  }

  const { existing, projectName } = link.data;
  return (
    <Button size="sm" asChild>
      <a
        href={link.data.url}
        target="_blank"
        rel="noopener noreferrer"
        title={
          existing
            ? `Open this server's Playground in ${projectName} to sign in and run its tools.`
            : `Add this server to ${projectName} in the dashboard, then sign in and run its tools in the Playground.`
        }
      >
        {existing ? "Open in Playground" : "Verify in Speakeasy"}
        <ExternalLinkIcon aria-hidden="true" />
      </a>
    </Button>
  );
}
