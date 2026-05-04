import { useState } from "react";
import { match } from "ts-pattern";
import { MODES, type Mode } from "@/lib/devidp";
import { LocalModePane } from "@/components/providers/LocalModePane";
import { WorkosModePane } from "@/components/providers/WorkosModePane";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

const LABELS: Record<Mode, string> = {
  "mock-speakeasy": "mock-speakeasy",
  "oauth2-1": "oauth2.1",
  oauth2: "oauth2",
  workos: "workos",
};

const SUBTITLES: Record<Mode, string> = {
  "mock-speakeasy":
    "Speakeasy provider exchange — backs Gram management-API login.",
  "oauth2-1": "OAuth 2.1 AS — PKCE required, DCR, OIDC.",
  oauth2: "OAuth 2.0 AS — PKCE optional, no DCR, OIDC.",
  workos: "Live WorkOS REST proxy — separate identity universe.",
};

export function ProvidersTab() {
  const [mode, setMode] = useState<Mode>("mock-speakeasy");

  return (
    <div className="max-w-5xl mx-auto">
      <Tabs
        orientation="vertical"
        value={mode}
        onValueChange={(v) => setMode(v as Mode)}
        className="flex-row gap-8"
      >
        <TabsList variant="line" className="min-w-[180px]">
          {MODES.map((m) => (
            <TabsTrigger key={m} value={m}>
              {LABELS[m]}
            </TabsTrigger>
          ))}
        </TabsList>
        <div className="flex-1 min-w-0">
          {MODES.map((m) => (
            <TabsContent key={m} value={m} className="mt-0">
              <header className="mb-6">
                <h2 className="text-lg font-semibold">{LABELS[m]}</h2>
                <p className="text-sm text-muted-foreground">{SUBTITLES[m]}</p>
              </header>
              {match(m)
                .with("workos", () => <WorkosModePane />)
                .otherwise((local) => (
                  <LocalModePane mode={local} />
                ))}
            </TabsContent>
          ))}
        </div>
      </Tabs>
    </div>
  );
}
