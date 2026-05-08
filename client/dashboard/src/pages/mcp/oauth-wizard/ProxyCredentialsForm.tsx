import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { Button, Stack } from "@speakeasy-api/moonshine";

import { WizardContext } from "./machine";

export function ProxyCredentialsForm() {
  const send = WizardContext.useActorRef().send;
  const proxy = WizardContext.useSelector((s) => s.context.proxy);
  const error = WizardContext.useSelector((s) => s.context.error);
  const submitting = WizardContext.useSelector((s) =>
    s.matches({ proxy: "submitting" }),
  );

  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        <div>
          <Type muted small className="mb-4">
            Enter the client credentials from your OAuth provider. These will be
            stored securely in a new environment created for this proxy.
          </Type>

          {error && <Type className="mb-4 text-sm text-red-500!">{error}</Type>}

          <Stack gap={4}>
            <div>
              <Type className="mb-2 font-medium">Client ID</Type>
              <Input
                placeholder="your-client-id"
                value={proxy.clientId}
                onChange={(value: string) =>
                  send({ type: "FIELD_PROXY", key: "clientId", value })
                }
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Client Secret</Type>
              <Input
                placeholder="your-client-secret"
                value={proxy.clientSecret}
                onChange={(value: string) =>
                  send({ type: "FIELD_PROXY", key: "clientSecret", value })
                }
                type="password"
              />
            </div>
          </Stack>
        </div>
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button variant="secondary" onClick={() => send({ type: "BACK" })}>
          Back
        </Button>
        <div className="ml-auto">
          <Button
            onClick={() => send({ type: "SUBMIT" })}
            disabled={
              submitting || !proxy.clientId.trim() || !proxy.clientSecret.trim()
            }
          >
            {submitting ? "Configuring..." : "Configure OAuth Proxy"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}
