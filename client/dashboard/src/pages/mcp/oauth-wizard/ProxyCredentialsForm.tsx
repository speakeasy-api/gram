import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";

import { WizardContext } from "./machine";

export function ProxyCredentialsForm(): JSX.Element {
  const actorRef = WizardContext.useActorRef();
  const send = actorRef.send.bind(actorRef);
  const proxy = WizardContext.useSelector((s) => s.context.proxy);
  const error = WizardContext.useSelector((s) => s.context.error);
  const submitting = WizardContext.useSelector((s) =>
    s.matches({ proxy: "submitting" }),
  );

  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        <div>
          <Text muted small className="mb-4">
            Enter the client credentials from your OAuth provider. These will be
            stored securely in a new environment created for this proxy.
          </Text>

          {error && <Text className="mb-4 text-sm text-red-500!">{error}</Text>}

          <Stack gap={4}>
            <div>
              <Text className="mb-2 font-medium">Client ID</Text>
              <Input
                placeholder="your-client-id"
                value={proxy.clientId}
                onChange={(value: string) =>
                  send({ type: "FIELD_PROXY", key: "clientId", value })
                }
              />
            </div>

            <div>
              <Text className="mb-2 font-medium">Client Secret</Text>
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
