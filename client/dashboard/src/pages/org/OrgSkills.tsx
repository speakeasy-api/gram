import { SettingsPage } from "@/components/page-templates";
import { SkillContentUploadSetting } from "./SkillContentUploadSetting";
import { SkillEfficacySettingsSection } from "./SkillEfficacySettingsSection";

export default function OrgSkills(): JSX.Element {
  return (
    <SettingsPage
      scope="org:admin"
      title="Skills"
      description="Configure organization-wide skill capture and efficacy sampling."
    >
      <SkillContentUploadSetting className="border-border bg-card max-w-2xl border p-6" />
      <SkillEfficacySettingsSection />
    </SettingsPage>
  );
}
