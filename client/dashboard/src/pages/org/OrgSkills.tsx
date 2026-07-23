import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
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
              <Heading variant="h4" className="mb-2">
                Skills
              </Heading>
              <Type muted small className="mb-6 max-w-2xl">
                Configure organization-wide skill capture and efficacy sampling.
              </Type>

              <div className="border-border bg-card max-w-2xl rounded-lg border p-6">
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
