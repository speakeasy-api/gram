import { Alert } from "@/components/ui/Alert";
import { CodeBlock } from "./CodeBlock";
import { Links, VariableField } from "./SetupFields";
import type { Connection, PlatformSetup } from "./setupInstructions";
import {
  platformEndpoints,
  subjectRule,
  type CatalogEntry,
  type CatalogVariable,
} from "./workloadCatalog";

const GKE_DOCS =
  "https://cloud.google.com/kubernetes-engine/docs/concepts/service-accounts";

// Where the pod finds its Speakeasy token. Its own directory, so it never
// collides with the service account token Kubernetes mounts by default.
const TOKEN_PATH = "/var/run/secrets/speakeasy/token";

const CLUSTERS_COMMAND =
  'gcloud container clusters list --format="table(name,location)"';
const ISSUER_COMMAND =
  "kubectl get --raw /.well-known/openid-configuration | jq -r .issuer";

/**
 * The pod spec's additions: the service account the rule names, and a token
 * for it issued for Speakeasy's audience. Values not known yet stay as
 * placeholders the reader can see are placeholders.
 */
function podManifest(values: Record<string, string>, audience?: string) {
  const namespace = values["namespace"]?.trim() || "NAMESPACE";
  const serviceAccount = values["service_account"]?.trim() || "SERVICE_ACCOUNT";
  return [
    "apiVersion: v1",
    "kind: Pod",
    "metadata:",
    "  name: my-workload",
    `  namespace: ${namespace}`,
    "spec:",
    `  serviceAccountName: ${serviceAccount}`,
    "  containers:",
    "    - name: app",
    "      image: my-image",
    "      env:",
    "        - name: SPEAKEASY_TOKEN_FILE",
    `          value: ${TOKEN_PATH}`,
    "      volumeMounts:",
    "        - name: speakeasy-token",
    "          mountPath: /var/run/secrets/speakeasy",
    "          readOnly: true",
    "  volumes:",
    "    - name: speakeasy-token",
    "      projected:",
    "        sources:",
    "          - serviceAccountToken:",
    "              path: token",
    `              audience: ${audience ?? "SPEAKEASY_AUDIENCE"}`,
    "              expirationSeconds: 600",
  ].join("\n");
}

/** The exchange, run inside the pod each time it needs a Speakeasy token. */
function exchangeScript(tokenEndpoint?: string) {
  return [
    `TOKEN_ENDPOINT=${tokenEndpoint ?? "SPEAKEASY_TOKEN_ENDPOINT"}`,
    "MCP_URL=https://your-mcp-server-url",
    "",
    "# Read the file each time: Kubernetes replaces the token before it expires.",
    'token=$(curl -sS --fail-with-body "$TOKEN_ENDPOINT" \\',
    '  --data-urlencode "grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer" \\',
    '  --data-urlencode "assertion=$(cat "$SPEAKEASY_TOKEN_FILE")" \\',
    "  | jq -r .access_token)",
    "",
    'curl -sS --fail-with-body "$MCP_URL" \\',
    '  -H "Authorization: Bearer $token" \\',
    '  -H "Content-Type: application/json" \\',
    '  -H "Accept: application/json, text/event-stream" \\',
    '  -d \'{"jsonrpc":"2.0","id":1,"method":"tools/list"}\'',
  ].join("\n");
}

/**
 * Google Kubernetes Engine.
 *
 * Every cluster is its own issuer, so the reader first says which cluster, and
 * that cluster becomes a trusted platform of its own. The rule then names one
 * namespace and service account on it: a pod running as that account, and
 * nothing else on the cluster, acts as this agent.
 */
export function gkeSetupInstructions(entry: CatalogEntry): PlatformSetup {
  const variable = (key: string): CatalogVariable | undefined =>
    entry.variables.find((candidate) => candidate.key === key);
  const fields = (keys: string[]) =>
    keys
      .map(variable)
      .filter((found): found is CatalogVariable => found !== undefined);
  const clusterFields = fields(["project", "location", "cluster"]);
  const workloadFields = fields(["namespace", "service_account"]);

  return {
    connection: (values): Connection | null => {
      const endpoints = platformEndpoints(entry, values);
      const rule = subjectRule(entry, values);
      if (endpoints === null || rule === null) return null;
      const project = values["project"]?.trim() ?? "";
      const location = values["location"]?.trim() ?? "";
      const cluster = values["cluster"]?.trim() ?? "";
      return {
        // One trusted platform per cluster, and their names must differ.
        name: `GKE ${cluster} (${project}, ${location})`,
        description: entry.description,
        issuer: endpoints.issuer,
        jwksUri: endpoints.jwksUri,
        subject: rule.subject,
        matchKind: rule.matchKind,
      };
    },
    requirements: (
      <div className="space-y-3 text-sm">
        <p>
          <strong>A GKE cluster</strong>, and <code>kubectl</code> access to it.
        </p>
        <p>
          <strong>Permission to edit the workload&apos;s manifests</strong>, to
          add the token Speakeasy accepts.
        </p>
        <Links links={[{ href: GKE_DOCS, label: "GKE service accounts" }]} />
      </div>
    ),
    steps: [
      {
        id: "cluster",
        short: "Cluster",
        title: "Your cluster",
        phase: "collect",
        complete: ({ values }) => platformEndpoints(entry, values) !== null,
        body: ({ values, setValue }) => {
          const endpoints = platformEndpoints(entry, values);
          return (
            <>
              <p className="text-sm">
                Each cluster signs its own tokens, so Speakeasy trusts one
                cluster at a time. List your clusters and their locations:
              </p>
              <CodeBlock
                value={CLUSTERS_COMMAND}
                label="Terminal"
                language="shell"
              />
              {clusterFields.map((field) => (
                <VariableField
                  key={field.key}
                  variable={field}
                  value={values[field.key] ?? ""}
                  onChange={(next) => setValue(field.key, next)}
                />
              ))}
              {endpoints !== null && (
                <div className="space-y-3 text-sm">
                  <p>
                    With <code>kubectl</code> pointed at the cluster, this
                    prints its issuer:
                  </p>
                  <CodeBlock
                    value={ISSUER_COMMAND}
                    label="Check the cluster"
                    language="shell"
                  />
                  <p>
                    It should read <code>{endpoints.issuer}</code>. If it does
                    not, check the project, location and cluster name above.
                  </p>
                </div>
              )}
            </>
          );
        },
      },
      {
        id: "workload",
        short: "Workload",
        title: "Your workload",
        // Saving needs only this and the agent, which the wizard is making.
        phase: "create",
        complete: ({ values }) => subjectRule(entry, values) !== null,
        body: ({ values, setValue, ruleConflict }) => (
          <>
            <p className="text-sm">
              The namespace and Kubernetes service account the workload&apos;s
              pods run as. Pods running as any other account are refused.
            </p>
            {workloadFields.map((field) => (
              <VariableField
                key={field.key}
                variable={field}
                value={values[field.key] ?? ""}
                onChange={(next) => setValue(field.key, next)}
              />
            ))}
            {ruleConflict !== null && (
              <Alert variant="error">
                <div className="text-sm">{ruleConflict}</div>
              </Alert>
            )}
          </>
        ),
      },
      {
        id: "manifest",
        short: "Pod manifest",
        title: "Give the pod a Speakeasy token",
        phase: "connect",
        body: ({ values, computed, issuerPicker }) => (
          <>
            <p className="text-sm">
              Add a projected token to the workload&apos;s pod spec. Kubernetes
              issues it for Speakeasy&apos;s audience and replaces it before it
              expires, so nothing secret is stored in the cluster.
            </p>
            <CodeBlock
              value={podManifest(values, computed.issuer_url)}
              label="Pod spec"
              language="yaml"
            />
            <p className="text-muted-foreground text-sm">
              In a Deployment, the same lines go under{" "}
              <code>spec.template.spec</code>. The audience must match exactly:
              Speakeasy refuses a token issued for any other.
            </p>
            {/* Speakeasy accepts each token once, and Kubernetes writes a new
                one at about 80% of its lifetime. At 600 seconds that is well
                inside the 15-minute access token, so the next exchange always
                finds an unused token. */}
            <p className="text-muted-foreground text-sm">
              Keep <code>expirationSeconds</code> at 600. Speakeasy accepts each
              token once, and at 600 seconds Kubernetes writes a fresh one about
              every 8 minutes, before the access token runs out.
            </p>
            {issuerPicker}
          </>
        ),
      },
      {
        id: "exchange",
        short: "Exchange",
        title: "Exchange the token and call the server",
        phase: "connect",
        body: ({ computed }) => (
          <>
            <p className="text-sm">
              Before calling an MCP server, the workload exchanges its token for
              a Speakeasy access token. Set <code>MCP_URL</code> to the
              server&apos;s URL, from its page in Speakeasy.
            </p>
            <CodeBlock
              value={exchangeScript(computed.token_endpoint)}
              label="Inside the pod"
              language="shell"
            />
            <p className="text-muted-foreground text-sm">
              The access token lasts up to 15 minutes and cannot be refreshed.
              When it runs out, read the file again and exchange the new token.
              Exchange from one place in the pod: each token is accepted once.
            </p>
          </>
        ),
      },
      {
        id: "check",
        short: "Run it",
        title: "Check it worked",
        phase: "connect",
        body: () => (
          <div className="space-y-3 text-sm">
            <p>
              Run the exchange in the pod. The second request prints the tools
              this agent&apos;s policy lets the workload reach.
            </p>
            <p>
              If the exchange answers <code>invalid_grant</code>, check that the
              pod runs as the namespace and service account above, and that the
              token&apos;s audience matches exactly.
            </p>
            <p>
              Each pod that signs in appears under the agent&apos;s sessions.
            </p>
          </div>
        ),
      },
    ],
  };
}
