import type { ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";

/**
 * API scopes the administrator grants the Okta app. Read access covers the
 * directory and the application inventory; the single write scope is what
 * lets Speakeasy create the sign-in application in the next sub-step.
 */
const OKTA_API_SCOPES = [
  "okta.groups.read",
  "okta.users.read",
  "okta.apps.read",
  "okta.apps.manage",
];

/**
 * A control or value named exactly as the Okta Admin Console spells it. The
 * administrator is reading our instructions with their console open, so these
 * mirror that screen rather than anything in our own model.
 */
function ConsoleLabel({ children }: { children: ReactNode }): JSX.Element {
  return <span className="text-foreground font-mono text-xs">{children}</span>;
}

function ConsoleStep({
  index,
  children,
}: {
  index: number;
  children: ReactNode;
}): JSX.Element {
  return (
    <li className="flex gap-3">
      <div
        aria-hidden="true"
        className="border-border text-muted-foreground flex h-6 w-6 flex-shrink-0 items-center justify-center border text-xs font-semibold"
      >
        {index}
      </div>
      <div className="min-w-0 flex-1 space-y-1">{children}</div>
    </li>
  );
}

interface OktaConnectSectionProps {
  /** Returns the card to the provider grid with nothing selected. */
  onChangeProvider: () => void;
}

// The first guided sub-step: what the administrator does in the Okta console
// before Speakeasy can read anything. Everything above the marked boundary is
// the explanation of the ceremony and holds no state — the tenant URL, the
// values Speakeasy prints, the deep links into the console, the client ID that
// comes back and the capability check are added below it.
export function OktaConnectSection({
  onChangeProvider,
}: OktaConnectSectionProps): JSX.Element {
  return (
    <div className="space-y-6">
      <Button
        variant="tertiary"
        size="sm"
        onClick={onChangeProvider}
        className="text-muted-foreground hover:text-foreground -ml-2 gap-1.5"
      >
        <ArrowLeft className="h-4 w-4" />
        Choose a different provider
      </Button>

      <Alert variant="warning" alignTop>
        <div>
          <AlertTitle>This step needs an Okta Super Administrator</AlertTitle>
          <AlertDescription>
            Granting API scopes to a service app is a Super Administrator action
            in Okta. If that is not you, the four console steps below have to go
            to someone who holds that role — everything after this step is back
            in Speakeasy.
          </AlertDescription>
        </div>
      </Alert>

      <div className="border-border bg-card border p-5">
        <h4 className="text-foreground text-sm leading-5 font-semibold">
          What you will do in the Okta Admin Console
        </h4>
        <p className="text-muted-foreground mt-1 text-sm">
          Four steps, once. Nothing secret comes back to Speakeasy.
        </p>

        <ol className="mt-5 space-y-5">
          <ConsoleStep index={1}>
            <p className="text-foreground text-sm">
              Create an <ConsoleLabel>API Services</ConsoleLabel> app
              integration.
            </p>
            <p className="text-muted-foreground text-sm">
              Applications › Create App Integration › API Services.
            </p>
          </ConsoleStep>

          <ConsoleStep index={2}>
            <p className="text-foreground text-sm">
              Set client authentication to{" "}
              <ConsoleLabel>Public key / Private key</ConsoleLabel>, choose{" "}
              <ConsoleLabel>Use a URL</ConsoleLabel>, and paste the address
              Speakeasy publishes its public keys at.
            </p>
            <p className="text-muted-foreground text-sm">
              Okta reads the key from that address, so rotating it later never
              means opening Okta again.
            </p>
          </ConsoleStep>

          <ConsoleStep index={3}>
            <p className="text-foreground text-sm">
              Grant these API scopes on the app&apos;s Okta API Scopes tab.
            </p>
            <div className="flex flex-wrap gap-1.5 pt-1">
              {OKTA_API_SCOPES.map((scope) => (
                <Badge
                  key={scope}
                  variant="neutral"
                  size="sm"
                  className="tracking-normal normal-case"
                >
                  <Badge.Text>{scope}</Badge.Text>
                </Badge>
              ))}
            </div>
            <p className="text-muted-foreground text-sm">
              One write scope in the whole flow.{" "}
              <ConsoleLabel>okta.apps.manage</ConsoleLabel> is what lets
              Speakeasy create the sign-in application for you in the next step;
              leave it out and you configure that application by hand instead.
            </p>
          </ConsoleStep>

          <ConsoleStep index={4}>
            <p className="text-foreground text-sm">
              Assign the app the{" "}
              <ConsoleLabel>Read-only Administrator</ConsoleLabel> and{" "}
              <ConsoleLabel>Application Administrator</ConsoleLabel> roles.
            </p>
            <p className="text-muted-foreground text-sm">
              Okta requires an admin role on an API Services app before the
              scopes take effect.
            </p>
          </ConsoleStep>
        </ol>
      </div>

      <Alert variant="info" alignTop>
        <div>
          <AlertTitle>There is no secret to hand over</AlertTitle>
          <AlertDescription>
            Okta authenticates this kind of app with a public key rather than a
            shared secret, so nothing sensitive is pasted in either direction.
            Speakeasy&apos;s private key stays in Speakeasy and is never
            displayed, exported, or returned by anything.
          </AlertDescription>
        </div>
      </Alert>

      {/* The exchange itself is added here: tenant URL, the values Speakeasy
          prints, deep links into this tenant, the identifier that comes back
          and the capability check that proves each scope. */}
    </div>
  );
}
