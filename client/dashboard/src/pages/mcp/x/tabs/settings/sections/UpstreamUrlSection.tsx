import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { validateMcpServerUrl } from "@/lib/sources";
import { useVerifyRemoteMcpUrl } from "@/pages/sources/remote-mcp/useVerifyRemoteMcpUrl";
import {
  VerifyRemoteMcpUrlAlert,
  VerifyRemoteMcpUrlButton,
} from "@/pages/sources/remote-mcp/VerifyRemoteMcpUrlButton";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { useUpdateRemoteMcpServerMutation } from "@gram/client/react-query/updateRemoteMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { invalidateRemoteMcpSourceViews } from "./sourceInvalidation";

export const MCP_UPSTREAM_URL_SECTION_ID = "upstream-url";

// Ported from the retired remote source page. The remote's slug is recomputed
// from its URL server-side, but this page is addressed by the mcp_server's
// own slug, so a URL change no longer needs a route replace.
export function UpstreamUrlSection({
  remoteMcpServer,
}: {
  remoteMcpServer: RemoteMcpServer;
}): JSX.Element {
  const initialUrl = remoteMcpServer.url;
  const [draft, setDraft] = useState(initialUrl);
  const [touched, setTouched] = useState(false);

  // When the upstream URL changes (e.g. another tab edited it), reset the
  // local draft so the input reflects the canonical value.
  useEffect(() => {
    setDraft(initialUrl);
    setTouched(false);
  }, [initialUrl]);

  const queryClient = useQueryClient();
  const update = useUpdateRemoteMcpServerMutation();
  const verify = useVerifyRemoteMcpUrl(draft);

  const urlError = validateMcpServerUrl(draft);
  const validationError = touched ? urlError : null;
  const dirty = draft.trim() !== initialUrl;
  const saveDisabled = !dirty || update.isPending || urlError !== null;
  const verifyDisabled = update.isPending || urlError !== null;

  const handleSave = async () => {
    setTouched(true);
    if (urlError !== null) return;
    try {
      await update.mutateAsync({
        request: {
          updateServerForm: { id: remoteMcpServer.id, url: draft.trim() },
        },
      });
      await invalidateRemoteMcpSourceViews(queryClient);
      toast.success("Upstream URL updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update URL";
      toast.error(message);
    }
  };

  const fieldError = validationError ?? update.error?.message;

  return (
    <SettingsSection id={MCP_UPSTREAM_URL_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Upstream URL</SettingsSection.Title>
        <SettingsSection.Description>
          The remote MCP endpoint this server proxies to. Must be an absolute
          http or https URL, usually ending in /mcp.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field
            data-invalid={fieldError ? true : undefined}
            className="max-w-xl"
          >
            <FieldLabel htmlFor="mcp-upstream-url">Remote MCP URL</FieldLabel>
            <Input
              id="mcp-upstream-url"
              value={draft}
              onChange={(value) => {
                setDraft(value);
                if (!touched) setTouched(true);
              }}
              onBlur={() => setTouched(true)}
              placeholder="https://example.com/mcp"
              disabled={update.isPending}
              aria-invalid={fieldError ? true : undefined}
            />
            {fieldError && <FieldError>{fieldError}</FieldError>}
            <VerifyRemoteMcpUrlAlert state={verify} />
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Verify before saving to confirm the endpoint answers as an MCP
            server.
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <VerifyRemoteMcpUrlButton
                state={verify}
                url={draft}
                disabled={verifyDisabled}
              />
              <FooterSaveButton
                pending={update.isPending}
                disabled={saveDisabled}
                onClick={() => void handleSave()}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
