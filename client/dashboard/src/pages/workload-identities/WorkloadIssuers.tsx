import { InlineEmptyState } from "@/components/inline-empty-state";
import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Card, Cards } from "@/components/ui/Card";
import { Badge } from "@/components/ui/Badge";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import {
  invalidateAllWorkloadIdentities,
  useWorkloadIdentities,
} from "@gram/client/react-query/workloadIdentities.js";
import { useRegisterWorkloadIssuerMutation } from "@gram/client/react-query/registerWorkloadIssuer.js";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";
import { Link, Outlet } from "react-router";
import { useRoutes } from "@/routes";
import { toast } from "sonner";
import {
  RegisterIssuerDialog,
  type RegisterIssuerValues,
} from "./RegisterIssuerDialog";

/**
 * The platforms this organization has trusted, one card each, and a way to trust
 * another.
 *
 * A card carries what an operator needs to recognise a platform and to judge its
 * reach: the identifier an assertion's `iss` must carry, where its keys are
 * published, and which tier it was trusted at. The machines actually allowed
 * under it live on that platform's own page, because those are per-machine
 * rather than per-platform.
 *
 * Platform presets will land in this same grid — see the administrator UI
 * milestone. A preset carries the values an operator cannot be expected to know
 * for a platform: its issuer identifier, its JWKS URL, and whether its subject
 * shape makes wildcard admission sound.
 */
export function WorkloadIssuersRoot(): JSX.Element {
  return <Outlet />;
}

export function WorkloadIssuersPage(): JSX.Element {
  return (
    <RequireScope scope={["workload:read", "workload:write"]} level="page">
      <WorkloadIssuersCatalogue />
    </RequireScope>
  );
}

function WorkloadIssuersCatalogue(): JSX.Element {
  const queryClient = useQueryClient();
  const [registerOpen, setRegisterOpen] = useState(false);
  // Catalog leads: the question an operator arrives with is which platform they
  // are connecting, and the answer is a preset where one exists. Private is the
  // fallback for a platform the catalogue does not carry yet — which today is
  // all of them.
  const [view, setView] = useState<IssuerView>("catalog");
  const { data, isPending } = useWorkloadIdentities({});
  const issuers = data?.issuers ?? [];

  const registerIssuer = useRegisterWorkloadIssuerMutation({
    onSuccess: async () => {
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setRegisterOpen(false);
      toast.success("Issuer trusted");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to trust the issuer",
      );
    },
  });

  const handleRegister = (values: RegisterIssuerValues) => {
    registerIssuer.mutate({
      request: {
        registerWorkloadIssuerForm: {
          name: values.name.trim(),
          issuer: values.issuer.trim(),
          jwksUri: values.jwksUri.trim(),
        },
      },
    });
  };

  const registerButton = (
    <RequireScope scope="workload:write" level="component">
      <Button size="sm" onClick={() => setRegisterOpen(true)}>
        <Button.LeftIcon>
          <Plus className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Register new access</Button.Text>
      </Button>
    </RequireScope>
  );

  return (
    <>
      <ResourceListPage
        title="Access Hub"
        description="The platforms whose identity tokens this organization will accept in exchange for a Gram session. Trusting one allows nothing on its own — open a platform to allow the machines that may use it."
        primaryAction={registerButton}
      >
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          gap={4}
          className="mb-6"
        >
          <SegmentedControl
            value={view}
            onChange={setView}
            options={[
              { value: "catalog", label: "Catalog" },
              { value: "custom", label: `Private (${issuers.length})` },
            ]}
          />
          <Text muted small className="min-w-0 text-right">
            {view === "custom"
              ? "Added by hand, with values from the platform's own console."
              : "Platforms Gram knows how to federate with, ready to trust without looking anything up."}
          </Text>
        </Stack>

        {view === "custom" ? (
          issuers.length === 0 && !isPending ? (
            <Cards noGrid>
              <InlineEmptyState
                icon="cpu"
                heading="No private platforms yet"
                description="Trust the platform that issues your machines\u2019 identity tokens, using the identifier and key URL from its console. Nothing is allowed until you then allow a machine under it."
              />
            </Cards>
          ) : (
            <Cards isLoading={isPending} cardSize={2}>
              {issuers.map((issuer) => (
                <IssuerCard key={issuer.id} issuer={issuer} />
              ))}
            </Cards>
          )
        ) : (
          // Empty on purpose. A preset carries what an operator cannot be
          // expected to know for a platform: its issuer identifier, its JWKS URL,
          // and whether its subject shape makes wildcard admission sound — the
          // judgement that otherwise falls to whoever is onboarding, every time.
          <Cards noGrid>
            <InlineEmptyState
              icon="layout-grid"
              heading="No catalog platforms yet"
              description="Presets for common platforms will appear here, each carrying that platform's issuer identifier, JWKS URL and whether its subjects can safely be matched by a wildcard. Until then, trust the platform as a private one."
            />
          </Cards>
        )}
      </ResourceListPage>

      <RegisterIssuerDialog
        open={registerOpen}
        onOpenChange={setRegisterOpen}
        onSubmit={handleRegister}
        isPending={registerIssuer.isPending}
      />
    </>
  );
}

type IssuerView = "catalog" | "custom";

function IssuerCard({ issuer }: { issuer: WorkloadIssuer }): JSX.Element {
  const routes = useRoutes();
  // project_id carries the tier: an organization-tier issuer is usable by every
  // project, a project-tier one only by the project it names.
  const organizationTier = issuer.projectId === "";

  return (
    // A link rather than an onClick, so the card keeps what a link gives for
    // free: middle-click, open in a new tab, and a focus ring for the keyboard.
    <Link
      to={routes.workloadIssuers.issuerDetail.href(issuer.id)}
      className="block h-full focus-visible:outline-2 focus-visible:outline-offset-2"
    >
      <Card className="hover:border-foreground/30 h-full transition-colors">
        <Card.Header>
          <Card.Title>{issuer.name}</Card.Title>
          <Card.Description className="break-all">
            {issuer.issuer}
          </Card.Description>
        </Card.Header>
        <Card.Content>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="neutral" background>
              {organizationTier ? "Organization" : "This project"}
            </Badge>
          </div>
          <Text muted small className="mt-3 block break-all">
            Keys: {issuer.jwksUri}
          </Text>
        </Card.Content>
      </Card>
    </Link>
  );
}
