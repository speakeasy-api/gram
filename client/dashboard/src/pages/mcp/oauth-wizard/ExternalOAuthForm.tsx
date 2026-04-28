import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Link } from "@/components/ui/link";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { Button, Stack } from "@speakeasy-api/moonshine";

import type { WizardSend } from "./machine";
import type { Context, DiscoveredOAuth } from "./machine-types";

export function ExternalOAuthForm({
  external,
  submitting,
  discovered,
  hasMultipleOAuth2AuthCode,
  oauth2SecurityCount,
  send,
}: {
  external: Context["external"];
  submitting: boolean;
  discovered: DiscoveredOAuth | null;
  hasMultipleOAuth2AuthCode: boolean;
  oauth2SecurityCount: number;
  send: WizardSend;
}) {
  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        {hasMultipleOAuth2AuthCode && (
          <div className="mb-4 rounded-md border border-red-200 bg-red-50 p-4">
            <Type small className="mt-1 text-red-600">
              Not Supported: This MCP server has {oauth2SecurityCount} OAuth2
              security schemes detected.
            </Type>
          </div>
        )}
        {discovered && !external.prefilled && (
          <div className="border-border bg-muted/50 mb-4 flex items-start justify-between gap-4 rounded-md border p-4">
            <div>
              <Type small className="font-medium">
                OAuth detected from {discovered.name}
              </Type>

              <Type muted small className="mt-1">
                We discovered OAuth {discovered.version} metadata from this
                server. You can use it to pre-fill the form below.
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
        {external.prefilled && (
          <div className="border-border bg-muted/50 mb-4 rounded-md border p-4">
            <Type small className="font-medium">
              Pre-filled from detected OAuth metadata
            </Type>
            <Type muted small className="mt-1">
              This form has been pre-filled with information Speakeasy detected
              about this server's OAuth requirements. Please review carefully
              and refer to the MCP server or API's documentation to confirm
              these values are correct.
            </Type>
          </div>
        )}
        <div>
          <Type className="mb-2 font-medium">
            External OAuth Server Configuration
          </Type>
          <Type muted small className="mb-4">
            Configure your MCP server to use an external authorization server if
            your API fits the very specific MCP OAuth requirements.{" "}
            <Link
              external
              to="https://docs.getgram.ai/host-mcp/adding-oauth#authorization-code"
            >
              Docs
            </Link>
          </Type>

          <Stack gap={4}>
            <div>
              <Type className="mb-2 font-medium">OAuth Server Slug</Type>
              <Input
                placeholder="my-oauth-server"
                value={external.slug}
                onChange={(value: string) =>
                  send({ type: "FIELD_EXTERNAL", key: "slug", value })
                }
                maxLength={40}
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">
                OAuth Authorization Server Metadata
              </Type>
              {external.jsonError && (
                <Type className="mt-1 text-sm text-red-500!">
                  {external.jsonError}
                </Type>
              )}
              <TextArea
                placeholder={`{
  "issuer": "https://your-oauth-server.com",
  "authorization_endpoint": "https://your-oauth-server.com/oauth/authorize",
  "registration_endpoint": "https://your-oauth-server.com/oauth/register",
  "token_endpoint": "https://your-oauth-server.com/oauth/token",
  "scopes_supported": ["read", "write"],
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code"],
  "token_endpoint_auth_methods_supported": [
    "client_secret_post"
  ],
  "code_challenge_methods_supported": [
    "plain",
    "S256"
  ]
}`}
                value={external.metadataJson}
                onChange={(value: string) =>
                  send({ type: "FIELD_EXTERNAL", key: "metadataJson", value })
                }
                rows={12}
                className="font-mono text-sm"
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
              hasMultipleOAuth2AuthCode ||
              submitting ||
              !external.slug.trim() ||
              !external.metadataJson.trim()
            }
          >
            {submitting ? "Configuring..." : "Configure External OAuth"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}
