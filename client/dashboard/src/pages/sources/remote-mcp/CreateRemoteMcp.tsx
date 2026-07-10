import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Alert, Button, Input, Stack } from "@/components/ui/moonshine";
import { AlertCircle, Loader2, Network } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useCreateRemoteMcpSource } from "./hooks";
import { useVerifyRemoteMcpUrl } from "./useVerifyRemoteMcpUrl";
import {
  VerifyRemoteMcpUrlAlert,
  VerifyRemoteMcpUrlButton,
} from "./VerifyRemoteMcpUrlButton";

// Mirrors server-side url.Parse: must be absolute, http(s), with a non-empty
// host. We surface this client-side so the user gets feedback before the round
// trip; the backend re-validates regardless.
function validateRemoteMcpUrl(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return "URL is required";
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return "Enter a valid absolute URL (e.g. https://example.com/mcp)";
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return "URL must use http or https";
  }
  if (!parsed.hostname) {
    return "URL must include a host";
  }
  return null;
}

export default function CreateRemoteMcp(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="mcp:write" level="page">
          <CreateRemoteMcpForm />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function CreateRemoteMcpForm() {
  const routes = useRoutes();
  const createSource = useCreateRemoteMcpSource();

  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  // Track whether the field has been touched so we don't surface "URL is
  // required" the moment the page renders.
  const [touched, setTouched] = useState(false);

  const verify = useVerifyRemoteMcpUrl(url);

  const validationError = touched ? validateRemoteMcpUrl(url) : null;
  const submitDisabled =
    createSource.isPending || !url.trim() || validateRemoteMcpUrl(url) !== null;
  const verifyDisabled =
    createSource.isPending || !url.trim() || validateRemoteMcpUrl(url) !== null;

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (validateRemoteMcpUrl(url) !== null) {
      return;
    }
    try {
      const trimmedName = name.trim();
      const { authAutoConfig, mcpServer } = await createSource.mutateAsync({
        name: trimmedName === "" ? undefined : trimmedName,
        url: url.trim(),
      });
      if (authAutoConfig.status === "configured") {
        toast.success("Remote MCP server added and authentication configured");
      } else {
        toast.success("Remote MCP server added");
        if (authAutoConfig.warn) {
          toast.warning(authAutoConfig.message);
        }
      }
      routes.mcp.x.overview.goTo(mcpServerRouteParam(mcpServer));
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to add remote MCP server";
      toast.error(message);
    }
  };

  return (
    <div className="max-w-2xl">
      <Stack gap={3} className="mb-8">
        <Stack direction="horizontal" gap={3} align="center">
          <div className="bg-muted flex h-10 w-10 shrink-0 items-center justify-center">
            <Network className="text-muted-foreground h-5 w-5" />
          </div>
          <Heading variant="h3">Add a custom remote MCP server</Heading>
        </Stack>
        <Type muted>
          Register an existing remote MCP server by URL. We&apos;ll proxy
          requests to it using streamable-http transport.
        </Type>
      </Stack>

      <form
        onSubmit={(e) => {
          void handleSubmit(e);
        }}
        noValidate
      >
        <Stack gap={4}>
          <Stack gap={1}>
            <label
              htmlFor="remote-mcp-name"
              className="text-sm leading-none font-medium"
            >
              Display name (optional)
            </label>
            <Input
              id="remote-mcp-name"
              autoFocus
              placeholder="My MCP server"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Stack>

          <Stack gap={1}>
            <label
              htmlFor="remote-mcp-url"
              className="text-sm leading-none font-medium"
            >
              Remote MCP server URL
            </label>
            <Input
              id="remote-mcp-url"
              placeholder="https://example.com/mcp"
              value={url}
              onChange={(e) => {
                setUrl(e.target.value);
                if (!touched) setTouched(true);
              }}
              onBlur={() => setTouched(true)}
              aria-invalid={validationError ? true : undefined}
              aria-describedby={
                validationError ? "remote-mcp-url-error" : undefined
              }
            />
            {validationError && (
              <div
                id="remote-mcp-url-error"
                role="alert"
                className="text-destructive mt-2 flex items-center gap-1.5 text-xs"
              >
                <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                <span>{validationError}</span>
              </div>
            )}
            <VerifyRemoteMcpUrlAlert state={verify} />
          </Stack>

          <Stack gap={1}>
            <label className="text-sm leading-none font-medium">
              Transport
            </label>
            <Type muted small>
              streamable-http
            </Type>
          </Stack>

          {createSource.isError && (
            <Alert variant="error" dismissible={false}>
              {createSource.error.message}
            </Alert>
          )}

          <Stack direction="horizontal" gap={2}>
            <VerifyRemoteMcpUrlButton
              state={verify}
              url={url}
              disabled={verifyDisabled}
            />
            <Button type="submit" variant="primary" disabled={submitDisabled}>
              {createSource.isPending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Adding</Button.Text>
                </>
              ) : (
                <Button.Text>Add server</Button.Text>
              )}
            </Button>
            <Button
              type="button"
              variant="secondary"
              disabled={createSource.isPending}
              onClick={() => routes.sources.goTo()}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </form>
    </div>
  );
}
