import {
  SettingsSection,
  FooterSaveButton,
} from "@/components/detail/settings-section";
import { useBuiltinMcpServers } from "@/components/mcp/useBuiltinMcpServers";
import { useUpdateBuiltinMcpServer } from "@/components/mcp/useUpdateBuiltinMcpServer";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Field, FieldLabel } from "@/components/ui/Field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import type { BuiltinMcpServer } from "@gram/client/models/components/builtinmcpserver.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { useState } from "react";
import { Link, useParams } from "react-router";

export default function BuiltinMcpServerPage(): JSX.Element | null {
  const organization = useOrganization();
  const project = useProject();
  return (
    <RequireScope scope="org:admin" resourceId={organization.id} level="page">
      <BuiltinMcpServerContent key={project.id} />
    </RequireScope>
  );
}

function BuiltinMcpServerContent() {
  const { builtinKey } = useParams<{ builtinKey: string }>();
  const { servers, isLoading, isError, refetch } = useBuiltinMcpServers();
  const routes = useRoutes();
  const server = servers.find((entry) => entry.key === builtinKey);
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ slack: "Slack" }}
          skipSegments={["builtin"]}
        />
      </Page.Header>
      <Page.Body>
        {isLoading ? (
          <Text muted>Loading built-in server…</Text>
        ) : isError ? (
          <div role="alert" className="space-y-3">
            <Text muted>Could not load this built-in server.</Text>
            <Button variant="secondary" onClick={() => void refetch()}>
              Retry
            </Button>
          </div>
        ) : !server ? (
          <Text muted>Built-in server not found.</Text>
        ) : (
          <>
            <Page.Section>
              <Page.Section.Title area="MCP Servers">
                {server.name}
              </Page.Section.Title>
              <Page.Section.CTA>
                <div className="flex items-center gap-3">
                  <Badge variant="neutral">Built-in</Badge>
                  {server.hidden && (
                    <Badge variant="neutral">Hidden from discovery</Badge>
                  )}
                </div>
              </Page.Section.CTA>
              <Page.Section.Description>
                {server.description}
              </Page.Section.Description>
              <Page.Section.Body>
                <Text small muted className="mt-3">
                  Available in every project. Administrators can configure this
                  server; human connections are blocked. Tool membership is
                  managed in code.
                </Text>
              </Page.Section.Body>
            </Page.Section>
            <SettingsSection>
              <SettingsSection.Header>
                <SettingsSection.Title>Stable endpoint</SettingsSection.Title>
              </SettingsSection.Header>
              <SettingsSection.Panel>
                <SettingsSection.Body>
                  <div className="flex items-center gap-2">
                    <code className="min-w-0 break-all text-sm">
                      {server.url}
                    </code>
                    <CopyButton text={server.url} tooltip="Copy MCP URL" />
                  </div>
                  <Text small muted>
                    Hiding this server does not change its endpoint or gateway
                    membership.{" "}
                    <Link
                      to={`${routes.settings.href()}#builtin-mcp-servers`}
                      className="underline"
                    >
                      Manage visibility in Project Settings
                    </Link>
                    .
                  </Text>
                </SettingsSection.Body>
              </SettingsSection.Panel>
            </SettingsSection>
            <BuiltinEnvironmentSettings
              key={`${server.id}:${server.environmentId ?? ""}`}
              server={server}
            />
            <SettingsSection>
              <SettingsSection.Header>
                <SettingsSection.Title>
                  Tools ({server.tools.length})
                </SettingsSection.Title>
                <SettingsSection.Description>
                  This read-only catalog lists the tools assigned to this
                  built-in server.
                </SettingsSection.Description>
              </SettingsSection.Header>
              <SettingsSection.Panel>
                <div className="divide-y">
                  {server.tools.map((tool) => (
                    <details key={tool.name} className="px-6 py-4">
                      <summary className="cursor-pointer font-mono text-sm">
                        {tool.name}
                      </summary>
                      <Text small muted className="mt-3 whitespace-pre-wrap">
                        {tool.description}
                      </Text>
                      <pre className="mt-3 max-h-80 overflow-auto bg-muted p-3 text-xs">
                        {formatSchema(tool.inputSchema)}
                      </pre>
                    </details>
                  ))}
                </div>
              </SettingsSection.Panel>
            </SettingsSection>
          </>
        )}
      </Page.Body>
    </Page>
  );
}

function formatSchema(schema: string) {
  try {
    return JSON.stringify(JSON.parse(schema), null, 2);
  } catch {
    return schema;
  }
}

function BuiltinEnvironmentSettings({ server }: { server: BuiltinMcpServer }) {
  const gramProject = useProjectSlugForRequests();
  const { data, isLoading, isError } = useListEnvironments(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const [environmentId, setEnvironmentId] = useState(
    server.environmentId ?? "none",
  );
  const update = useUpdateBuiltinMcpServer();
  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Credentials</SettingsSection.Title>
        <SettingsSection.Description>
          Only header and environment credentials are supported. No OAuth
          connection is created. Gateways use this project default unless their
          membership has an environment override.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field>
            <FieldLabel htmlFor="builtin-environment">
              Default environment
            </FieldLabel>
            <Select
              value={environmentId}
              onValueChange={setEnvironmentId}
              disabled={isLoading || isError || update.isPending}
            >
              <SelectTrigger id="builtin-environment" className="max-w-md">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">
                  Project default environment
                </SelectItem>
                {(data?.environments ?? []).map((environment) => (
                  <SelectItem key={environment.id} value={environment.id}>
                    {environment.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {isError && (
            <Text small className="text-destructive">
              Could not load project environments.
            </Text>
          )}
          <Text small muted>
            Store the required Slack credentials in the selected environment.
            Tokens are never shown here.
          </Text>
          <ul className="list-inside list-disc text-sm">
            {server.credentialVariables.map((variable) => (
              <li key={variable}>
                <code>{variable}</code>
              </li>
            ))}
          </ul>
          <Text small muted>
            Bot-capable tools prefer SLACK_BOT_TOKEN, then SLACK_USER_TOKEN,
            then SLACK_TOKEN. User-only tools require SLACK_USER_TOKEN or
            SLACK_TOKEN.
          </Text>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterActions>
            <FooterSaveButton
              pending={update.isPending}
              disabled={
                update.isPending ||
                environmentId === (server.environmentId ?? "none") ||
                isError
              }
              onClick={() =>
                update.save({
                  key: server.key,
                  expectedVersion: server.version,
                  hidden: server.hidden,
                  environmentId:
                    environmentId === "none" ? undefined : environmentId,
                })
              }
            />
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
