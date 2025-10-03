// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import tailwindcss from "@tailwindcss/vite";
import vercel from "@astrojs/vercel";
import starlightLlmsTxt from "starlight-llms-txt";
import starlightSidebarTopics from "starlight-sidebar-topics";

// https://astro.build/config
export default defineConfig({
  // We don't want trailing slashes in the generated page URLs so it's easy to
  // tack on '.md' and get the markdown version of a page.
  trailingSlash: "never",
  adapter: vercel(),
  site: "https://docs.getgram.ai",
  redirects: {
    "/concepts/tool-defintions": "/concepts/tool-definitions",
    "/how-does-it-work": "/introduction",
    "/blog/gram-concepts": "/introduction",
    "/blog/tool-curation": "/build-mcp/advanced-tool-curation",
    "/blog/the-easiest-way-to-host-mcp-servers": "/gram-quickstart",
    "/build-mcp/adding-oauth": "/host-mcp/adding-oauth",
    "/build-mcp/deploy-mcp-servers": "/host-mcp/deploy-mcp-servers",
    "/build-mcp/public-private-servers": "/host-mcp/public-private-servers",
  },
  vite: {
    server: {
      allowedHosts: ["localhost", "127.0.0.1", "devbox"],
    },
    plugins: [tailwindcss()],
  },

  integrations: [
    starlight({
      routeMiddleware: ["./src/route-data.ts"],

      title: "Gram",
      tagline: "Ship powerful integrations for agents and LLMs ",
      favicon: "/favicon.png",

      head: [
        {
          tag: "link",
          attrs: {
            rel: "icon",
            href: "/favicon.ico",
            type: "image/x-icon",
            sizes: "256x256",
          },
        },
        {
          tag: "link",
          attrs: {
            rel: "icon",
            href: "/favicon-dark.ico",
            media: "(prefers-color-scheme: dark)",
            type: "image/x-icon",
            sizes: "256x256",
          },
        },
      ],

      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/speakeasy-api/gram-docs",
        },
      ],
      components: {
        EditLink: "./src/components/edit-link.astro",
        Header: "./src/components/header.astro",
        Hero: "./src/components/hero.astro",
        Pagination: "./src/components/pagination.astro",
        TableOfContents: "./src/components/TableOfContents.astro",
      },
      editLink: {
        baseUrl: "https://github.com/speakeasy-api/gram-docs/edit/main/",
      },
      customCss: ["./src/fonts/font-face.css", "./src/styles/global.css"],
      plugins: [
        starlightSidebarTopics([
          {
            label: "Documentation",
            link: "/introduction/",
            items: [
              {
                label: "Start here",
                items: ["introduction", "gram-quickstart"],
              },
              {
                label: "Gram Concepts",
                autogenerate: { directory: "concepts" },
              },
              {
                label: "Build an MCP server",
                autogenerate: { directory: "build-mcp" },
              },
              {
                label: "Productionize an MCP server",
                autogenerate: { directory: "host-mcp" },
              },
              {
                label: "Command Line (beta)",
                items: [
                  {
                    label: "Installation",
                    link: "/command-line/installation",
                  },
                  {
                    label: "Use",
                    link: "/command-line/use",
                  },
                ],
              },
            ],
          },
          {
            label: "Guides",
            link: "/guides",
            items: [
              {
                label: "Guides",
                link: "/guides",
              },
              {
                label: "Integration Guides",
                autogenerate: { directory: "clients" },
              },
              {
                label: "API Usage",
                autogenerate: { directory: "api-clients" },
              },
              {
                label: "Examples",
                autogenerate: { directory: "examples" },
              },
            ],
          },
        ]),
        starlightLlmsTxt({ projectName: "gram" }),
      ],
    }),
  ],
});
