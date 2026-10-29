import { Alert } from "@/components/ui/Alert";
import { CodeBlock } from "./CodeBlock";
import { ConsoleRow } from "./ConsoleRow";
import { ExchangeValues } from "./GkeExchange";
import { Links, PendingValues, VariableField } from "./SetupFields";
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
    `              audience: ${audience || "SPEAKEASY_AUDIENCE"}`,
    "              expirationSeconds: 600",
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
            <ol className="border-border divide-border divide-y border">
              <ConsoleRow
                number={1}
                label="Issue the token for Speakeasy"
                fields={[
                  {
                    label: "Audience",
                    value: computed.issuer_url ?? "",
                    hint: "The pod spec below sets this as the token's audience.",
                  },
                ]}
              />
            </ol>
            {!computed.issuer_url && (
              <PendingValues placeholders={["SPEAKEASY_AUDIENCE"]}>
                {issuerPicker}
              </PendingValues>
            )}
            <CodeBlock
              value={podManifest(values, computed.issuer_url)}
              label="Pod spec"
              language="yaml"
            />
            <p className="text-muted-foreground text-sm">
              In a Deployment, <code>namespace</code> stays in the
              Deployment&apos;s own <code>metadata</code>, and the rest goes
              under <code>spec.template.spec</code>. The audience must match
              exactly: Speakeasy refuses a token issued for any other.
            </p>
            {/* Speakeasy accepts each token once, and Kubernetes writes a new
                one at about 80% of its lifetime. At 600 seconds that falls
                inside the 15-minute access token, so a workload that exchanges
                only when its access token runs out finds an unused token. */}
            <p className="text-muted-foreground text-sm">
              Keep <code>expirationSeconds</code> at 600. Speakeasy accepts each
              token once, and at 600 seconds Kubernetes writes a fresh one about
              every 8 minutes, before the access token runs out.
            </p>
            {computed.issuer_url && issuerPicker}
          </>
        ),
      },
      {
        id: "exchange",
        short: "Exchange",
        title: "Exchange the token and call the server",
        phase: "connect",
        body: ({ computed, servers, serversLeftOut, issuerPicker }) => (
          <>
            <ExchangeValues
              tokenEndpoint={computed.token_endpoint}
              servers={servers}
              serversLeftOut={serversLeftOut ?? 0}
              issuerPicker={issuerPicker}
            />
            <p className="text-muted-foreground text-sm">
              The access token lasts up to 15 minutes and cannot be refreshed.
              Reuse it until it runs out, then read the file again and exchange
              the new token. Exchanging again sooner can send a token Speakeasy
              has already accepted, which it refuses, so exchange from one place
              in the pod.
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
