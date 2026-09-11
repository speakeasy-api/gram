import { RequireScope } from "@/components/require-scope";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import type { ReactNode } from "react";

export function AuthenticationSetupActions({
  onStartManual,
  additionalAction,
}: {
  onStartManual: () => void;
  additionalAction?: ReactNode;
}): JSX.Element {
  return (
    <RequireScope scope="mcp:write" level="component">
      <div className="flex flex-col items-center gap-2">
        <Text muted small>
          OAuth metadata was not advertised by this server.
        </Text>
        <div className="flex flex-wrap items-center justify-center gap-2">
          <Button variant="secondary" onClick={onStartManual}>
            <Button.Text>Configure Manually</Button.Text>
          </Button>
          {additionalAction}
        </div>
      </div>
    </RequireScope>
  );
}
