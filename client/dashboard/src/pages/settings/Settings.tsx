import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useIsAdmin } from "@/contexts/Auth";
import { ShieldAlert } from "lucide-react";
import { Stack } from "@speakeasy-api/moonshine";
import { SettingsDangerZone } from "./SettingsDangerZone";
import { RegistryCacheSection } from "./RegistryCacheSection";

export default function Settings() {
  const isAdmin = useIsAdmin();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>Project Settings</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <Heading variant="h4" className="mb-2">
          Project Settings
        </Heading>
        <Type muted small className="mb-6">
          Manage your project configuration and perform administrative actions.
        </Type>
        <SettingsDangerZone />

        {isAdmin && (
          <div className="mt-8 p-4 rounded-lg bg-red-500/5 border border-red-500/20">
            <Stack
              direction="horizontal"
              align="center"
              gap={2}
              className="mb-3"
            >
              <ShieldAlert className="w-5 h-5 text-red-500" />
              <Heading variant="h4" className="text-red-600 dark:text-red-400">
                Admin Only
              </Heading>
            </Stack>
            <RegistryCacheSection />
          </div>
        )}
      </Page.Body>
    </Page>
  );
}
