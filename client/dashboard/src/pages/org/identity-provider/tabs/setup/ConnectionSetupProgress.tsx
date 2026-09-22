import { cn } from "@/lib/utils";

import {
  connectionStep,
  type ConnectionStep,
  type LiveConnection,
} from "../../connectionView";

const STEPS = [
  "Add your Okta organization",
  "Set up the Okta app",
  "Verify connection",
];

const CURRENT_STEP: Record<ConnectionStep, number | null> = {
  submit_client_id: 1,
  verify: 2,
  repair: 2,
  connected: null,
};

export function ConnectionSetupProgress({
  connection,
}: {
  connection: Pick<LiveConnection, "status" | "clientIdSubmitted">;
}): JSX.Element | null {
  const current = CURRENT_STEP[connectionStep(connection)];
  if (current === null) return null;
  return (
    <nav aria-label="Okta setup progress">
      <ol className="grid grid-cols-1 gap-2 sm:grid-cols-3">
        {STEPS.map((label, index) => (
          <li
            key={label}
            aria-current={index === current ? "step" : undefined}
            className={cn(
              "flex items-center gap-2 border-t-2 pt-2 text-sm",
              index === current
                ? "border-primary font-medium"
                : "border-border text-muted-foreground",
            )}
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
