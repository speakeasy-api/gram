import { RequireScope } from "@/components/require-scope";
import type { Check } from "@/lib/authz.gen";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import type { ReactNode } from "react";

export function AuthenticationSetupActions({
  writeCheck,
  onStartManual,
  additionalAction,
}: {
  writeCheck: Check;
  onStartManual: () => void;
  additionalAction?: ReactNode;
}): JSX.Element {
  return (
    <RequireScope check={writeCheck} level="component">
      <div className="flex flex-col items-center gap-2">
        <Text muted small>
          OAuth metadata was not advertised by this server.
        </Text>
        <div className="flex flex-wrap items-center justify-center gap-2">
          <Button variant="secondary" onClick={onStartManual}>
            <Button.Text>Configure manually</Button.Text>
          </Button>
          {additionalAction}
        </div>
      </div>
    </RequireScope>
  );
}
