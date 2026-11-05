import { render } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { ConfigProvider } from "@/components/ui/context/ConfigContext";
import { SetupGuideIcon } from "./SetupGuideIcon";
vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: () => ({
    data: {
      servers: [
        {
          registrySpecifier: "app.linear/linear",
          remotes: [{ url: "https://example.com/mcp" }],
          iconUrl: "legacy",
          meta: {
            "com.speakeasy.ai/icons": {
              default: { src: "default" },
              dark: { src: "dark" },
            },
          },
        },
      ],
    },
  }),
}));
it("resolves setup headers by identity and responds to active theme", () => {
  const view = (theme: "light" | "dark") => (
    <ConfigProvider theme={theme} setTheme={() => {}}>
      <SetupGuideIcon registrySpecifier="app.linear/linear" iconUrl="legacy" />
    </ConfigProvider>
  );
  const { container, rerender } = render(view("light"));
  expect(container.querySelector("img")?.getAttribute("src")).toBe("default");
  rerender(view("dark"));
  expect(container.querySelector("img")?.getAttribute("src")).toBe("dark");
  expect(container.querySelector("img")?.className).not.toContain("invert");
  rerender(<SetupGuideIcon serverUrl="https://example.com/mcp/" />);
  expect(container.querySelector("img")?.getAttribute("src")).toBe("default");
});
