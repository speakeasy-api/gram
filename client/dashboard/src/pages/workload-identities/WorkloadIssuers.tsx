import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { cn } from "@/lib/utils";
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
import { useMemo, useState } from "react";
import { Link, Outlet } from "react-router";
import { useRoutes } from "@/routes";
import { toast } from "sonner";
import { issuerMatches } from "./search";
import {
  RegisterIssuerSheet,
  type RegisterIssuerValues,
} from "./RegisterIssuerSheet";

/**
 * The platforms this organization has trusted, one card each, and a way to trust
 * another.
 *
 * A card carries what an operator needs to recognise a platform and to judge its
 * reach: the identifier an assertion's `iss` must carry, where its keys are
 * published, and its tags. The machines allowed under it live on that
 * platform's own page, because those are per-machine rather than per-platform.
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
  // are connecting, and the answer is a preset where one exists. Custom is the
  // fallback for a platform the catalogue does not carry.
  const [view, setView] = useState<IssuerView>("catalog");
  const [search, setSearch] = useState("");
  const { data, isPending } = useWorkloadIdentities({});
  const issuers = useMemo(() => data?.issuers ?? [], [data]);

  const visibleIssuers = useMemo(
    () => issuers.filter((issuer) => issuerMatches(issuer, search)),
    [issuers, search],
  );

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
          description: values.description.trim() || undefined,
          issuer: values.issuer.trim(),
          jwksUri: values.jwksUri.trim(),
          tags: values.tags,
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
        description="Platform workloads, such as CI jobs, cloud services and AI agents running on other platforms, sign in to Gram with identity tokens from the platforms registered here. Registering a platform admits nothing on its own. Open it to admit the specific platform workloads that may connect and assign each one an agent whose permissions it inherits."
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
              { value: "custom", label: `Custom (${issuers.length})` },
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
                heading="No custom platforms yet"
                description="Trust the platform that issues your machines\u2019 identity tokens, using the identifier and key URL from its console. Nothing is allowed until you then allow a machine under it."
              />
            </Cards>
          ) : (
            <>
              <Page.Toolbar className="mb-4">
                <Page.Toolbar.Search
                  className="w-full"
                  value={search}
                  onChange={setSearch}
                  placeholder="Search name, description, URL or tag…"
                />
              </Page.Toolbar>
              {visibleIssuers.length === 0 && !isPending ? (
                <Cards noGrid>
                  <InlineEmptyState
                    icon="search"
                    heading="No platforms match"
                    description="Nothing registered here matches that search. Try part of a name, an issuer URL or a tag."
                  />
                </Cards>
              ) : (
                <Cards isLoading={isPending} cardSize={2}>
                  {visibleIssuers.map((issuer) => (
                    <IssuerCard key={issuer.id} issuer={issuer} />
                  ))}
                </Cards>
              )}
            </>
          )
        ) : (
          // A preset carries what an operator cannot be expected to know for a
          // platform: its issuer identifier, its JWKS URL, and whether its
          // subject shape makes wildcard admission sound.
          <Cards noGrid>
            <InlineEmptyState
              icon="layout-grid"
              heading="No catalog platforms yet"
              description="Presets for common platforms will appear here, each carrying that platform's issuer identifier, JWKS URL and whether its subjects can safely be matched by a wildcard. Until then, register the platform as a custom one."
            />
          </Cards>
        )}
      </ResourceListPage>

      <RegisterIssuerSheet
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
  // An operator's description says which platform this is far better than its
  // URL does; the URL moves down beside the keys when there is one.
  const hasDescription = issuer.description !== "";

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
          <Card.Description
            className={
              hasDescription ? "line-clamp-2 whitespace-normal" : "break-all"
            }
          >
            {hasDescription ? issuer.description : issuer.issuer}
          </Card.Description>
        </Card.Header>
        <Card.Content>
          {issuer.tags.length > 0 && (
            <div className="flex flex-wrap items-center gap-2">
              {issuer.tags.map((tag) => (
                <Badge key={tag} variant="information">
                  {tag}
                </Badge>
              ))}
            </div>
          )}
          {hasDescription && (
            <Text muted small className="mt-3 block break-all">
              Issuer: {issuer.issuer}
            </Text>
          )}
          <Text
            muted
            small
            className={cn("block break-all", hasDescription ? "mt-1" : "mt-3")}
          >
            Keys: {issuer.jwksUri}
          </Text>
        </Card.Content>
      </Card>
    </Link>
  );
}
