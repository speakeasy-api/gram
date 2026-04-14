import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { AlertTriangle } from "lucide-react";
import React from "react";

import type { WizardDispatch, WizardState } from "./types";

export function ProxyCredentialsForm({
  state,
  dispatch,
  isSubmitting,
  onSubmit,
  attachedEnvironmentName,
  environmentsLink,
}: {
  state: Extract<WizardState, { step: "oauth_proxy_client_credentials_form" }>;
  dispatch: WizardDispatch;
  isSubmitting: boolean;
  onSubmit: () => void;
  attachedEnvironmentName: string | null;
  environmentsLink: React.ReactNode;
}) {
  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        <div>
          <Type muted small className="mb-4">
            Enter the client credentials from your OAuth provider. These will be
            stored securely in a new environment created for this proxy.
          </Type>

          {attachedEnvironmentName && (
            <div className="border-border bg-muted/50 mb-4 flex items-start gap-3 rounded-md border p-4">
              <AlertTriangle className="text-muted-foreground mt-0.5 h-4 w-4 shrink-0" />
              <div>
                <Type small className="font-medium">
                  Existing environment will be detached
                </Type>
                <Type muted small className="mt-1">
                  The environment "{attachedEnvironmentName}" is currently
                  attached to this MCP server. It will be detached and replaced
                  with a new environment containing these OAuth credentials.
                </Type>
                <div className="mt-2">{environmentsLink}</div>
              </div>
            </div>
          )}

          {state.error && (
            <Type className="mb-4 text-sm text-red-500!">{state.error}</Type>
          )}

          <Stack gap={4}>
            <div>
              <Type className="mb-2 font-medium">Client ID</Type>
              <Input
                placeholder="your-client-id"
                value={state.clientId}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "clientId",
                    value: v,
                  })
                }
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Client Secret</Type>
              <Input
                placeholder="your-client-secret"
                value={state.clientSecret}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "clientSecret",
                    value: v,
                  })
                }
                type="password"
              />
            </div>
          </Stack>
        </div>
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button variant="secondary" onClick={() => dispatch({ type: "BACK" })}>
          Back
        </Button>
        <div className="ml-auto">
          <Button
            onClick={onSubmit}
            disabled={
              isSubmitting ||
              !state.clientId.trim() ||
              !state.clientSecret.trim()
            }
          >
            {isSubmitting ? "Configuring..." : "Configure OAuth Proxy"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}
