import { useState } from "react";
import { HomeTab } from "@/components/HomeTab";
import { ProvidersTab } from "@/components/ProvidersTab";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

type TabId = "home" | "providers";

export function App() {
  const [tab, setTab] = useState<TabId>("home");

  return (
    <div className="min-h-screen flex flex-col">
      <header className="border-b border-border px-6 py-4">
        <div className="flex items-center justify-between gap-6 max-w-6xl mx-auto">
          <h1 className="text-lg font-semibold tracking-tight">dev-idp</h1>
          <Tabs
            value={tab}
            onValueChange={(v) => setTab(v as TabId)}
            className="!gap-0"
          >
            <TabsList>
              <TabsTrigger value="home">Home</TabsTrigger>
              <TabsTrigger value="providers">Providers</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
      </header>
      <main className="flex-1 px-6 py-6">
        {tab === "home" ? <HomeTab /> : <ProvidersTab />}
      </main>
    </div>
  );
}
