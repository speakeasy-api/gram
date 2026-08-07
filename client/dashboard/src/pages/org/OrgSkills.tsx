import { PageEyebrow } from "@/components/page-eyebrow";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { SkillContentUploadSetting } from "./SkillContentUploadSetting";
import { SkillEfficacySettingsSection } from "./SkillEfficacySettingsSection";

export default function OrgSkills(): JSX.Element {
  const { data: features } = useProductFeatures(undefined, undefined, {
    staleTime: 30_000,
    throwOnError: false,
  });

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          {features?.skillsEnabled === true && (
            <>
              <div className="mb-6">
                <PageEyebrow className="mb-2" />
                <Heading
                  variant="h4"
                  className="mb-2 text-display-sm font-thin"
                >
                  Skills
                </Heading>
                <Text muted small className="mt-1 max-w-2xl">
                  Configure organization-wide skill capture and efficacy
                  sampling.
                </Text>
              </div>

              <div className="border-border bg-card max-w-2xl border p-6">
                <SkillContentUploadSetting />
              </div>

              <SkillEfficacySettingsSection />
            </>
          )}
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
