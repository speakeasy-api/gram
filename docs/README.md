<a href="https://www.speakeasy.com/product/gram" target="_blank">
   <picture>
       <source media="(prefers-color-scheme: light)" srcset="https://github.com/user-attachments/assets/1812f171-1650-4045-ac35-21bdd7b103a6">
       <source media="(prefers-color-scheme: dark)" srcset="https://github.com/user-attachments/assets/3f14e446-0dec-4b8a-b36e-fd92efc25751">
       <img src="https://github.com/user-attachments/assets/3f14e446-0dec-4b8a-b36e-fd92efc25751#gh-dark-mode-only" alt="Gram">
   </picture>
 </a>

# Gram Docs

[![Built with Starlight](https://astro.badg.es/v2/built-with-starlight/tiny.svg)](https://starlight.astro.build)

This repository contains the documentation for the [Gram](https://app.getgram.ai) app.

To run

```
cp .env-example .env
npm i
npm run dev
```

Now you can visit the project locally at <http://localhost:4321>.

## Publishing model: synced to Speakeasy marketing

This docs site is used for local authoring only. On deploy, all routes are redirected to the Speakeasy marketing site at `http://speakeasy.com/docs/gram/` (see `vercel.json`). A GitHub Action (`.github/workflows/sync-docs-to-marketing.yaml`) automatically syncs content to the marketing repo and opens a PR.

- What syncs: files under `docs/src/content/docs/**` and public assets under `docs/public/**`.
- Where it lands: `speakeasy-api/marketing-site` at `src/content/docs/gram/` and assets at `public/assets/docs/gram/`.
- Excluded: `docs/src/content/docs/index.mdx` (the landing page is owned by marketing).
- When it runs: on pushes to `main` that touch the paths above, or via manual dispatch.

### Authoring guidelines (important)

- No Starlight components: do not use any Starlight UI components (e.g., Tabs, Card, Aside, Steps, or Starlight-specific imports). They won’t render on the Speakeasy marketing site. Stick to plain Markdown/MDX.
- Assets location: place images and videos in `docs/public/` (e.g., `docs/public/img/...`, `docs/public/videos/...`).
  - Reference them with root-relative paths: `/img/...` or `/videos/...`.
  - The sync workflow rewrites these to `/assets/docs/gram/img/...` and `/assets/docs/gram/videos/...` in marketing.
- Internal links: use relative links or root-relative links starting with `/...`. Root-relative links are rewritten to `/docs/gram/...` during sync (non-asset paths only).
- Keep landing page separate: avoid editing `docs/src/content/docs/index.mdx`; marketing maintains the top-level landing page.

You can still use the Astro + Starlight site locally to develop and preview, but assume only plain Markdown/MDX will make it to production.

## 🚀 Project Structure

Inside this Astro + Starlight project, you'll see the following folders and files:

```
.
├── public/                # Static assets that will _not_ be processed by Astro
├── src/
│   ├── assets/            # Images that will be optimized by Astro
│   ├── components/        # Shared components that override Starlight's default components
│   ├── content/           # All the content for the site
│   ├── fonts/             # Fonts used throughout the site
│   ├── pages/             # Additional pages or special paths to be added to the site
│   ├── styles/global.css  # Tailwind theme configuration and startlight style overrides
│   ├── content.config.ts  # Configuration for content collections
│   └── route-data.ts      # Middleware for injecting metadata into site routes
├── astro.config.mjs       # Astro configuration
├── package.json           # Dependencies
└── tsconfig.json          # TypeScript configuration
```

Starlight looks for `.md` or `.mdx` files in the `src/content/docs/` directory. Each file is exposed as a route based on its file name.

For assets that must appear on the marketing site, place them under `docs/public/` (prefer `docs/public/img` and `docs/public/videos`) and reference with root-relative paths as described above. Assets placed in `src/assets/` will not be synced to marketing.

## 🧞 Commands

All commands are run from the root of the project, from a terminal:

| Command                | Action                                           |
| :--------------------- | :----------------------------------------------- |
| `pnpm install`         | Installs dependencies                            |
| `pnpm dev`             | Starts local dev server at `localhost:4321`      |
| `pnpm build`           | Build your production site to `./dist/`          |
| `pnpm preview`         | Preview your build locally, before deploying     |
| `pnpm astro ...`       | Run CLI commands like `astro add`, `astro check` |
| `pnpm astro -- --help` | Get help using the Astro CLI                     |

## 👀 Built with Starlight

Check out [Starlight’s docs](https://starlight.astro.build/), read [the Astro documentation](https://docs.astro.build), or jump into the [Astro Discord server](https://astro.build/chat).
