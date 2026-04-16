import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { Button, Stack } from "@speakeasy-api/moonshine";

import type { WizardDispatch, WizardState } from "./types";

export function ProxyCredentialsForm({
  state,
  dispatch,
  isSubmitting,
  onSubmit,
}: {
  state: Extract<WizardState, { step: "oauth_proxy_client_credentials_form" }>;
  dispatch: WizardDispatch;
  isSubmitting: boolean;
  onSubmit: () => void;
}) {
  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        <div>
          <Type muted small className="mb-4">
            Enter the client credentials from your OAuth provider. These will be
            stored securely in a new environment created for this proxy.
          </Type>

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
