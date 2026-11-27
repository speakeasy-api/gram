import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Link } from "@/components/ui/Link";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";

import { WizardContext } from "./machine";
import type { ProxyFormKey } from "./machine-types";

export function ProxyMetadataForm(): JSX.Element {
  const actorRef = WizardContext.useActorRef();
  const send = actorRef.send.bind(actorRef);
  const proxy = WizardContext.useSelector((s) => s.context.proxy);
  const error = WizardContext.useSelector((s) => s.context.error);

  const setField = (key: ProxyFormKey, value: string) =>
    send({ type: "FIELD_PROXY", key, value });

  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        <div>
          <Text muted small className="mb-2 font-medium">
            Ideal for internal MCP servers. The OAuth Proxy configuration can be
            used to set up auth for an MCP server even though the underlying API
            doesn't support MCP OAuth.
          </Text>
          <Text muted small className="mb-4 font-medium">
            Getting proxy settings correct can be tricky. Need help?
            <Link
              href="https://calendly.com/d/ctgg-5dv-3kw/intro-to-gram-call"
              target="_blank"
            >
              Book a meeting
            </Link>
          </Text>

          {error && <Text className="mb-4 text-sm text-red-500!">{error}</Text>}

          <Stack gap={4}>
            <div>
              <Text className="mb-2 font-medium">OAuth Proxy Server Slug</Text>
              <Input
                placeholder="my-oauth-proxy"
                value={proxy.slug}
                onChange={(v: string) => setField("slug", v)}
                maxLength={40}
              />
            </div>

            <div>
              <Text className="mb-2 font-medium">Authorization Endpoint</Text>
              <Input
                placeholder="https://provider.com/oauth/authorize"
                value={proxy.authorizationEndpoint}
                onChange={(v: string) => setField("authorizationEndpoint", v)}
              />
            </div>

            <div>
              <Text className="mb-2 font-medium">Token Endpoint</Text>
              <Input
                placeholder="https://provider.com/oauth/token"
                value={proxy.tokenEndpoint}
                onChange={(v: string) => setField("tokenEndpoint", v)}
              />
            </div>

            <div>
              <Text className="mb-2 font-medium">
                Scopes (comma-separated, optional)
              </Text>
              <Input
                placeholder="read, write, openid"
                value={proxy.scopes}
                onChange={(v: string) => setField("scopes", v)}
              />
            </div>

            <div>
              <Text className="mb-2 font-medium">Audience (optional)</Text>
              <Input
                placeholder="https://api.example.com"
                value={proxy.audience}
                onChange={(v: string) => setField("audience", v)}
              />
              <Text muted small className="mt-1">
                The audience parameter sent to the upstream OAuth provider.
                Required by some providers (e.g. Auth0) to return JWT access
                tokens.
              </Text>
            </div>

            <div>
              <Text className="mb-2 font-medium">
                Token Endpoint Auth Method
              </Text>
              <select
                className="bg-background w-full border px-3 py-2"
                value={proxy.tokenAuthMethod}
                onChange={(e) => setField("tokenAuthMethod", e.target.value)}
              >
                <option value="client_secret_basic">client_secret_basic</option>
                <option value="client_secret_post">client_secret_post</option>
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
