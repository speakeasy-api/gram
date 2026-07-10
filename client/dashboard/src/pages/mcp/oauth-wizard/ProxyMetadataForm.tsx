import { Dialog } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { Alert, Button, Input, Link, Stack } from "@/components/ui/moonshine";

import { WizardContext } from "./machine";
import type { ProxyFormKey } from "./machine-types";

export function ProxyMetadataForm(): JSX.Element {
  const actorRef = WizardContext.useActorRef();
  const send = actorRef.send.bind(actorRef);
  const proxy = WizardContext.useSelector((s) => s.context.proxy);
  const error = WizardContext.useSelector((s) => s.context.error);
  const discovered = WizardContext.useSelector((s) => s.context.discovered);

  const setField = (key: ProxyFormKey, value: string) =>
    send({ type: "FIELD_PROXY", key, value });

  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        <div>
          <Type muted small className="mb-2 font-medium">
            Ideal for internal MCP servers. The OAuth Proxy configuration can be
            used to set up auth for an MCP server even though the underlying API
            doesn't support MCP OAuth.
          </Type>
          <Type muted small className="mb-4 font-medium">
            Getting proxy settings correct can be tricky. Need help?
            <Link
              href="https://calendly.com/d/ctgg-5dv-3kw/intro-to-gram-call"
              rel="noopener noreferrer"
            >
              Book a meeting
            </Link>
          </Type>

          {discovered && !proxy.prefilled && (
            <Alert variant="info" dismissible={false}>
              <div className="flex w-full items-start justify-between gap-4">
                <div>
                  <Type small className="font-medium">
                    OAuth detected from {discovered.name}
                  </Type>
                  <Type muted small className="mt-1">
                    We discovered OAuth {discovered.version} metadata from this
                    server. You can use it to pre-fill the endpoints below.
                  </Type>
                </div>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => send({ type: "APPLY_DISCOVERED" })}
                >
                  Apply
                </Button>
              </div>
            </Alert>
          )}
          {proxy.prefilled && (
            <Alert variant="info" dismissible={false}>
              <Type small className="font-medium">
                Pre-filled from detected OAuth metadata
              </Type>
              <Type muted small className="mt-1">
                This form has been pre-filled with information Speakeasy
                detected about this server's OAuth requirements. Please review
                carefully and refer to the MCP server or API's documentation to
                confirm these values are correct.
              </Type>
            </Alert>
          )}

          {error && (
            <Alert variant="error" dismissible={false}>
              {error}
            </Alert>
          )}

          <Stack gap={4}>
            <Field>
              <FieldLabel>OAuth Proxy Server Slug</FieldLabel>
              <Input
                placeholder="my-oauth-proxy"
                value={proxy.slug}
                onChange={(e) => setField("slug", e.target.value)}
                maxLength={40}
              />
            </Field>

            <Field>
              <FieldLabel>Authorization Endpoint</FieldLabel>
              <Input
                placeholder="https://provider.com/oauth/authorize"
                value={proxy.authorizationEndpoint}
                onChange={(e) =>
                  setField("authorizationEndpoint", e.target.value)
                }
              />
            </Field>

            <Field>
              <FieldLabel>Token Endpoint</FieldLabel>
              <Input
                placeholder="https://provider.com/oauth/token"
                value={proxy.tokenEndpoint}
                onChange={(e) => setField("tokenEndpoint", e.target.value)}
              />
            </Field>

            <Field>
              <FieldLabel optional>Scopes (comma-separated)</FieldLabel>
              <Input
                placeholder="read, write, openid"
                value={proxy.scopes}
                onChange={(e) => setField("scopes", e.target.value)}
              />
            </Field>

            <Field>
              <FieldLabel optional>Audience</FieldLabel>
              <Input
                placeholder="https://api.example.com"
                value={proxy.audience}
                onChange={(e) => setField("audience", e.target.value)}
              />
              <FieldDescription>
                The audience parameter sent to the upstream OAuth provider.
                Required by some providers (e.g. Auth0) to return JWT access
                tokens.
              </FieldDescription>
            </Field>

            <Field>
              <FieldLabel>Token Endpoint Auth Method</FieldLabel>
              <Select
                value={proxy.tokenAuthMethod}
                onValueChange={(value) => setField("tokenAuthMethod", value)}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="client_secret_basic">
                    client_secret_basic
                  </SelectItem>
                  <SelectItem value="client_secret_post">
                    client_secret_post
                  </SelectItem>
                  <SelectItem value="none">none</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </Stack>
        </div>
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button variant="secondary" onClick={() => send({ type: "BACK" })}>
          Back
        </Button>
        <div className="ml-auto">
          <Button
            onClick={() => send({ type: "NEXT" })}
            disabled={
              !proxy.slug.trim() ||
              !proxy.authorizationEndpoint.trim() ||
              !proxy.tokenEndpoint.trim()
            }
          >
            Next
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}
