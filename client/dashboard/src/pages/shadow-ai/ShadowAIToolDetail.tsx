import { IdentityLink } from "@/components/identity-link";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { AIToolDecisionSheet } from "@/components/shadow-ai/AIToolDecisionSheet";
import { detectionEvidenceColumns } from "@/components/shadow-ai/detectionColumns";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { shadowAIBreadcrumbSubstitutions } from "@/pages/shadow-ai/tabs";
import type { AIDetectionUser } from "@gram/client/models/components/aidetectionuser.js";
import { useAiDetectionUsers } from "@gram/client/react-query/aiDetectionUsers.js";
import { useState } from "react";
import { useParams } from "react-router";

// The identity page's Shadow AI table turned around: there, one person and
// a row per tool; here, one tool and a row per person, with the same
// evidence columns after the first.
const COLUMNS: Column<AIDetectionUser>[] = [
  {
    key: "user",
    header: "User",
    width: "1.2fr",
    render: (user) => (
      <Text small className="truncate font-medium">
        <IdentityLink identifier={{ email: user.userEmail }}>
          {user.userEmail}
        </IdentityLink>
      </Text>
    ),
  },
  ...detectionEvidenceColumns<AIDetectionUser>(),
];

// Organization admin to view, gated outside the component that reads: the
// read behind this page names people across the organization and requires
// org:admin on the server, so a narrower gate would only show an error.
export default function ShadowAIToolDetail(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <ShadowAIToolDetailPage />
    </RequireScope>
  );
}

function ShadowAIToolDetailPage(): JSX.Element {
  const { targetId = "" } = useParams<{ targetId: string }>();
  const usersQuery = useAiDetectionUsers({ targetId }, undefined, {
    enabled: targetId.length > 0,
    throwOnError: false,
  });
  const detection = usersQuery.data?.detection;
  const users = usersQuery.data?.users ?? [];
  const [deciding, setDeciding] = useState(false);
  // A local model never connects to the gateway, so there is nothing to
  // decide about it, the same reason the Local Models tab records no decision.
  const canDecide =
    detection !== undefined && detection.category !== "local_model";

  let content: JSX.Element;
  if (usersQuery.isPending) {
    content = <SkeletonTable />;
  } else if (usersQuery.isError) {
    content = (
      <ErrorAlert
        title="Unable to load Shadow AI detections"
        error={usersQuery.error}
      />
    );
  } else if (users.length === 0) {
    content = (
      <InlineEmptyState
        icon="radar"
        heading="No detected users"
        description="No enrolled device has reported this tool."
        orientation="horizontal"
      />
    );
  } else {
    content = (
      <div className="overflow-x-auto">
        <Table
          columns={COLUMNS}
          data={users}
          rowKey={(user) => user.userEmail}
          className="min-w-[820px]"
        />
      </div>
    );
  }

  // No area eyebrow on the title, as on the tab pages this is reached from.
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            ...shadowAIBreadcrumbSubstitutions,
            [targetId]: detection?.displayName ?? targetId,
          }}
        />
      </Page.Header>
      <Page.Body fullHeight className="pb-8">
        <Page.Section>
          <Page.Section.Title area="">
            {detection?.displayName ?? targetId}
          </Page.Section.Title>
          <Page.Section.Description>
            Enrolled users this tool was detected for, from device-agent scans
            across the organization.
          </Page.Section.Description>
          <Page.Section.CTA>
            {canDecide && (
              <Button onClick={() => setDeciding(true)}>Decide access</Button>
            )}
          </Page.Section.CTA>
          <Page.Section.Body>
            <AIToolDecisionSheet
              detection={detection ?? null}
              open={deciding}
              onOpenChange={setDeciding}
            />
            {content}
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}
