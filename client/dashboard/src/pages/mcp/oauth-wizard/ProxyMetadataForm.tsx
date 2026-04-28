import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Link } from "@/components/ui/link";
import { Type } from "@/components/ui/type";
import { Button, Stack } from "@speakeasy-api/moonshine";

import type { DiscoveredOAuth, WizardDispatch, WizardState } from "./types";

export function ProxyMetadataForm({
  state,
  dispatch,
  discoveredOAuth,
  editMode,
  isEditPending,
  isNextPending,
  onNext,
  onEditSubmit,
  onClose,
}: {
  state: Extract<WizardState, { step: "oauth_proxy_server_metadata_form" }>;
  dispatch: WizardDispatch;
  discoveredOAuth: DiscoveredOAuth | null;
  editMode: boolean;
  isEditPending: boolean;
  isNextPending: boolean;
  onNext: () => void;
  onEditSubmit: () => void;
  onClose: () => void;
}) {
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

          {discoveredOAuth && !state.prefilled && (
            <div className="border-border bg-muted/50 mb-4 flex items-start justify-between gap-4 rounded-md border p-4">
              <div>
                <Type small className="font-medium">
                  OAuth detected from {discoveredOAuth.name}
                </Type>
                <Type muted small className="mt-1">
                  We discovered OAuth {discoveredOAuth.version} metadata from
                  this server. You can use it to pre-fill the endpoints below.
                </Type>
              </div>
              <Button
                size="sm"
                variant="secondary"
                onClick={() =>
                  dispatch({
                    type: "APPLY_DISCOVERED",
                    discoveredOAuth,
                  })
                }
              >
                Apply
              </Button>
            </div>
          )}
          {state.prefilled && (
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

          {state.error && (
            <Type className="mb-4 text-sm text-red-500!">{state.error}</Type>
          )}

          <Stack gap={4}>
            <div>
              <Type className="mb-2 font-medium">OAuth Proxy Server Slug</Type>
              <Input
                placeholder="my-oauth-proxy"
                value={state.slug}
                onChange={(v: string) =>
                  dispatch({ type: "UPDATE_FIELD", field: "slug", value: v })
                }
                maxLength={40}
                disabled={editMode}
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Authorization Endpoint</Type>
              <Input
                placeholder="https://provider.com/oauth/authorize"
                value={state.authorizationEndpoint}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "authorizationEndpoint",
                    value: v,
                  })
                }
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Token Endpoint</Type>
              <Input
                placeholder="https://provider.com/oauth/token"
                value={state.tokenEndpoint}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "tokenEndpoint",
                    value: v,
                  })
                }
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Scopes (comma-separated)</Type>
              <Input
                placeholder="read, write, openid"
                value={state.scopes}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "scopes",
                    value: v,
                  })
                }
              />
            </div>

            <div>
              <Type className="mb-2 font-medium">Audience (optional)</Type>
              <Input
                placeholder="https://api.example.com"
                value={state.audience}
                onChange={(v: string) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "audience",
                    value: v,
                  })
                }
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
                value={state.tokenAuthMethod}
                onChange={(e) =>
                  dispatch({
                    type: "UPDATE_FIELD",
                    field: "tokenAuthMethod",
                    value: e.target.value,
                  })
                }
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
        <Button
          variant="secondary"
          onClick={() => {
            if (editMode) {
              onClose();
            } else {
              dispatch({ type: "BACK" });
            }
          }}
        >
          {editMode ? "Cancel" : "Back"}
        </Button>
        <div className="ml-auto">
          <Button
            onClick={editMode ? onEditSubmit : onNext}
            disabled={
              (editMode && isEditPending) ||
              (!editMode && isNextPending) ||
              !state.slug.trim() ||
              !state.authorizationEndpoint.trim() ||
              !state.tokenEndpoint.trim()
            }
          >
            {editMode
              ? isEditPending
                ? "Saving..."
                : "Save changes"
              : isNextPending
                ? "Registering client..."
                : "Next"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}
