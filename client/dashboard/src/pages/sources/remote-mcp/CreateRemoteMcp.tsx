import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";
import { Loader2, Network } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useCreateRemoteMcpSource } from "./hooks";

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

export default function CreateRemoteMcp() {
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

  const [url, setUrl] = useState("");
  // Track whether the field has been touched so we don't surface "URL is
  // required" the moment the page renders.
  const [touched, setTouched] = useState(false);

  const validationError = touched ? validateRemoteMcpUrl(url) : null;
  const submitDisabled =
    createSource.isPending || !url.trim() || validateRemoteMcpUrl(url) !== null;

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (validateRemoteMcpUrl(url) !== null) {
      return;
    }
    try {
      const { remoteMcpServer } = await createSource.mutateAsync({
        url: url.trim(),
      });
      toast.success("Remote MCP server added");
      routes.sources.source.goTo("remotemcp", remoteMcpServer.id);
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
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-violet-500/10 dark:bg-violet-500/20">
            <Network className="h-5 w-5 text-violet-600 dark:text-violet-400" />
          </div>
          <Heading variant="h3">Add a custom remote MCP server</Heading>
        </Stack>
        <Type muted>
          Register an existing remote MCP server by URL. We&apos;ll proxy
          requests to it using streamable-http transport.
        </Type>
      </Stack>

      <form onSubmit={handleSubmit} noValidate>
        <Stack gap={4}>
          <Stack gap={1}>
            <label
              htmlFor="remote-mcp-url"
              className="text-sm leading-none font-medium"
            >
              Remote MCP server URL
            </label>
            <Input
              id="remote-mcp-url"
              autoFocus
              placeholder="https://example.com/mcp"
              value={url}
              onChange={(value) => {
                setUrl(value);
                if (!touched) setTouched(true);
              }}
              onBlur={() => setTouched(true)}
            />
            {validationError && (
              <Alert variant="error" dismissible={false}>
                {validationError}
              </Alert>
            )}
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
