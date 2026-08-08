import { FormPage } from "@/components/page-templates";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Stack } from "@/components/ui/Stack";
import { useIsSpeakeasyStaff } from "@/contexts/Auth";
import { mcpServerRouteParam, validateMcpServerUrl } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { AlertCircle, Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useCreateUnproxiedMcpSource } from "./hooks";

export default function CreateUnproxiedMcp(): JSX.Element {
  const isSpeakeasyStaff = useIsSpeakeasyStaff();
  const routes = useRoutes();

  // Route-guard in addition to hiding the dropdown entry: this page is
  // reachable by URL directly, and the backend rejects the create call
  // regardless, but a staff-only empty state here is clearer than watching a
  // form submit fail.
  if (!isSpeakeasyStaff) {
    return (
      <FormPage
        title="Speakeasy staff only"
        description="Unproxied MCP servers are restricted to Speakeasy staff while we validate this workflow."
      >
        <div>
          <Button variant="secondary" onClick={() => routes.sources.goTo()}>
            <Button.Text>Back to Sources</Button.Text>
          </Button>
        </div>
      </FormPage>
    );
  }

  return <CreateUnproxiedMcpForm />;
}

function CreateUnproxiedMcpForm() {
  const routes = useRoutes();
  const createSource = useCreateUnproxiedMcpSource();

  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [description, setDescription] = useState("");
  // Track whether the field has been touched so we don't surface "URL is
  // required" the moment the page renders.
  const [touched, setTouched] = useState(false);

  const validationError = touched ? validateMcpServerUrl(url) : null;
  const submitDisabled =
    createSource.isPending || !url.trim() || validateMcpServerUrl(url) !== null;

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (validateMcpServerUrl(url) !== null) {
      return;
    }
    try {
      const trimmedName = name.trim();
      const trimmedDescription = description.trim();
      const { mcpServer } = await createSource.mutateAsync({
        name: trimmedName === "" ? undefined : trimmedName,
        url: url.trim(),
        description: trimmedDescription === "" ? undefined : trimmedDescription,
      });
      toast.success("Unproxied MCP server added");
      routes.mcp.x.overview.goTo(mcpServerRouteParam(mcpServer));
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to add unproxied MCP server";
      toast.error(message);
    }
  };

  return (
    <FormPage
      scope="mcp:write"
      title={
        <>
          New unproxied MCP server{" "}
          <Badge variant="neutral">Speakeasy staff only</Badge>
        </>
      }
      description="List a vendor's MCP server without proxying it. Speakeasy never fetches this URL or manages OAuth for it — the customer connects to the vendor directly. Use this to sidestep per-vendor OAuth callback allowlisting for servers we don't need to proxy."
    >
      <form
        onSubmit={(e) => {
          void handleSubmit(e);
        }}
        noValidate
      >
        <Stack gap={4}>
          <Stack gap={1}>
            <label
              htmlFor="unproxied-mcp-name"
              className="text-sm leading-none font-medium"
            >
              Display name (optional)
            </label>
            <Input
              id="unproxied-mcp-name"
              autoFocus
              placeholder="Vendor's MCP server"
              value={name}
              onChange={setName}
            />
          </Stack>

          <Stack gap={1}>
            <label
              htmlFor="unproxied-mcp-url"
              className="text-sm leading-none font-medium"
            >
              Vendor MCP server URL
            </label>
            <Input
              id="unproxied-mcp-url"
              placeholder="https://vendor.example.com/mcp"
              value={url}
              onChange={(value) => {
                setUrl(value);
                if (!touched) setTouched(true);
              }}
              onBlur={() => setTouched(true)}
              aria-invalid={validationError ? true : undefined}
              aria-describedby={
                validationError ? "unproxied-mcp-url-error" : undefined
              }
            />
            {validationError && (
              <div
                id="unproxied-mcp-url-error"
                role="alert"
                className="text-destructive mt-2 flex items-center gap-1.5 text-xs"
              >
                <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                <span>{validationError}</span>
              </div>
            )}
          </Stack>

          <Stack gap={1}>
            <label
              htmlFor="unproxied-mcp-description"
              className="text-sm leading-none font-medium"
            >
              Description (optional)
            </label>
            <Input
              id="unproxied-mcp-description"
              placeholder="Shown alongside the server in the dashboard"
              value={description}
              onChange={setDescription}
            />
          </Stack>

          {createSource.isError && (
            <Alert variant="error" dismissible={false}>
              {createSource.error.message}
            </Alert>
          )}

          <Stack direction="horizontal" gap={2}>
            <Button type="submit" variant="primary" disabled={submitDisabled}>
              {createSource.isPending ? (
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>
                {createSource.isPending ? "Adding" : "Add server"}
              </Button.Text>
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
    </FormPage>
  );
}
