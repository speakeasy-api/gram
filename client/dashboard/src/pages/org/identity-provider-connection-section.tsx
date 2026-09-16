import type { ReactNode } from "react";
import { KeyRound } from "lucide-react";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import { useIdentityProvider } from "@gram/client/react-query/identityProvider.js";
import { IdentityProviderCapabilities } from "@/components/identity-provider-capabilities";
import { Badge } from "@/components/ui/Badge";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";

type StatusTone = "success" | "neutral" | "warning";

const STATUS_COPY: Record<string, { label: string; tone: StatusTone }> = {
  active: { label: "Connected", tone: "success" },
  pending: { label: "Setup started", tone: "neutral" },
  awaiting_verification: { label: "Waiting for a check", tone: "neutral" },
  failed: { label: "Last check did not pass", tone: "warning" },
};

/** Where sign-on has got to, in the administrator's terms. */
const SIGN_IN_COPY: Record<string, string> = {
  not_started: "The sign-in application has not been created yet.",
  application_created:
    "The sign-in application has been created in Okta, and sign-on has not been checked yet.",
  passed: "Sign-on is configured and has been checked.",
  failed: "The last sign-on check did not pass.",
};

function formatTimestamp(value: Date): string {
  return value.toLocaleString([], {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function Fact({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="space-y-0.5">
      <Text variant="small" muted>
        {label}
      </Text>
      <div>{children}</div>
    </div>
  );
}

function signInLine(connection: IdentityProviderConnection): string {
  const state = connection.signInState ?? "not_started";
  return SIGN_IN_COPY[state] ?? SIGN_IN_COPY["not_started"]!;
}

/**
 * The guided connection, read only. Setup itself lives on the setup task page;
 * this is where an administrator comes later to see what Speakeasy can do in
 * their tenant, which key Okta reads, and when any of it was last proved.
 */
export function IdentityProviderConnectionSection(): JSX.Element | null {
  const orgRoutes = useOrgRoutes();
  const { data } = useIdentityProvider(undefined, undefined, {
    throwOnError: false,
  });
  const connection = data?.connection;
  if (!connection) return null;

  const status = STATUS_COPY[connection.status] ?? {
    label: connection.status,
    tone: "neutral" as StatusTone,
  };
  // Nothing left to do only when the connection is live and sign-on has
  // proved out; anything earlier is still setup.
  const settled =
    connection.status === "active" && connection.signInState === "passed";
  const reads = connection.verifyEvidence?.reads ?? [];

  return (
    <section>
      <div className="flex flex-col">
        <Heading variant="h5" className="mb-1">
          Identity provider
        </Heading>
        <Text as="div" muted small className="mb-4">
          The Okta tenant Speakeasy is connected to, and what it is allowed to
          do there.
        </Text>
        <div className="border-border overflow-hidden border">
          <div className="flex items-center gap-4 p-4">
            <div className="bg-muted flex h-10 w-10 shrink-0 items-center justify-center rounded-full">
              <KeyRound className="text-muted-foreground h-5 w-5" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <Text variant="body" className="font-medium">
                  {connection.tenantIdentifier}
                </Text>
                <Badge variant={status.tone} background size="sm">
                  <Badge.Text>{status.label}</Badge.Text>
                </Badge>
              </div>
              <Text muted small>
                {signInLine(connection)}
              </Text>
            </div>
            <orgRoutes.setupTask.Link
              params={["idp"]}
              className="text-muted-foreground hover:text-foreground text-sm whitespace-nowrap underline underline-offset-4 transition-colors"
            >
              {settled ? "Review setup" : "Continue setup"}
            </orgRoutes.setupTask.Link>
          </div>

          <div className="border-border grid gap-4 border-t p-4 sm:grid-cols-2">
            <Fact label="Last checked">
              <Text>
                {connection.lastVerifiedAt
                  ? formatTimestamp(connection.lastVerifiedAt)
                  : "Not checked yet"}
              </Text>
            </Fact>
          </div>

          {reads.length > 0 ? (
            <div className="border-border border-t p-4">
              <Text variant="small" muted className="mb-2">
                What the last check proved
              </Text>
              <IdentityProviderCapabilities reads={reads} />
            </div>
          ) : null}
        </div>
      </div>
    </section>
  );
}
