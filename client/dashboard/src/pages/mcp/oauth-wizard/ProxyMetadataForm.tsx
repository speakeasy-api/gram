import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Link } from "@/components/ui/link";
import { Type } from "@/components/ui/type";
import { Button, Stack } from "@speakeasy-api/moonshine";

import { WizardContext } from "./machine";
import type { ProxyFormKey } from "./machine-types";

export function ProxyMetadataForm() {
  const send = WizardContext.useActorRef().send;
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
              external
              to="https://calendly.com/d/ctgg-5dv-3kw/intro-to-gram-call"
            >
              Book a meeting
            </Link>
          </Type>

          {discovered && !proxy.prefilled && (
            <div className="border-border bg-muted/50 mb-4 flex items-start justify-between gap-4 rounded-md border p-4">
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
          )}
          {proxy.prefilled && (
            <div className="border-border bg-muted/50 mb-4 rounded-md border p-4">
              <Type small className="font-medium">
                Pre-filled from detected OAuth metadata
              </Type>
              <Type muted small className="mt-1">
                This form has been pre-filled with information Speakeasy
                detected about this server's OAuth requirements. Please review
                carefully and refer to the MCP server or API's documentation to
                confirm these values are correct.
              </Type>
            </div>
          )}

          {error && <Type className="mb-4 text-sm text-red-500!">{error}</Type>}

          <Stack gap={4}>
            <div>
              <Type className="mb-2 font-medium">OAuth Proxy Server Slug</Type>
              <Input
                placeholder="my-oauth-proxy"
                value={proxy.slug}
                onChange={(v: string) => setField("slug", v)}
                maxLength={40}
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Authorization Endpoint</Type>
              <Input
                placeholder="https://provider.com/oauth/authorize"
                value={proxy.authorizationEndpoint}
                onChange={(v: string) => setField("authorizationEndpoint", v)}
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Token Endpoint</Type>
              <Input
                placeholder="https://provider.com/oauth/token"
                value={proxy.tokenEndpoint}
                onChange={(v: string) => setField("tokenEndpoint", v)}
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Scopes (comma-separated)</Type>
              <Input
                placeholder="read, write, openid"
                value={proxy.scopes}
                onChange={(v: string) => setField("scopes", v)}
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Audience (optional)</Type>
              <Input
                placeholder="https://api.example.com"
                value={proxy.audience}
                onChange={(v: string) => setField("audience", v)}
              />
              <Type muted small className="mt-1">
                The audience parameter sent to the upstream OAuth provider.
                Required by some providers (e.g. Auth0) to return JWT access
                tokens.
              </Type>
            </div>

            <div>
              <Type className="mb-2 font-medium">
                Token Endpoint Auth Method
              </Type>
              <select
                className="bg-background w-full rounded border px-3 py-2"
                value={proxy.tokenAuthMethod}
                onChange={(e) => setField("tokenAuthMethod", e.target.value)}
              >
                <option value="client_secret_post">client_secret_post</option>
                <option value="client_secret_basic">client_secret_basic</option>
                <option value="none">none</option>
              </select>
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
