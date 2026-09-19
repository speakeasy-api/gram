import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { connectionStep, isConnectionVerified } from "./connectionView";

const STEPS = [
  "Add your Okta organization",
  "Set up the Okta app",
  "Verify connection",
];

export function ConnectionSetupProgress({
  connection,
}: {
  connection?: Pick<
    OktaIdentityProviderConnection,
    "status" | "clientIdSubmitted"
  >;
}): JSX.Element | null {
  const step = connection ? connectionStep(connection) : undefined;
  if (connection && (isConnectionVerified(connection) || step === "revoked"))
    return null;
  let current = 0;
  if (step === "submit_client_id") current = 1;
  if (step === "verify" || connection?.status === "degraded") current = 2;
  return (
    <nav aria-label="Okta setup progress">
      <ol className="grid grid-cols-1 gap-2 sm:grid-cols-3">
        {STEPS.map((label, index) => (
          <li
            key={label}
            aria-current={index === current ? "step" : undefined}
            className={`flex items-center gap-2 border-t-2 pt-2 text-sm ${index === current ? "border-primary font-medium" : "border-border text-muted-foreground"}`}
          >
            <span className="font-mono text-xs" aria-hidden="true">
              {index < current ? "✓" : index + 1}
            </span>
            {label}
            {index < current && <span className="sr-only"> (complete)</span>}
          </li>
        ))}
      </ol>
    </nav>
  );
}
