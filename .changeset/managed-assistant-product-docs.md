---
"server": patch
---

The project assistant can now read the AI control plane product documentation. Two new managed-assistant tools, `platform_list_docs` and `platform_get_doc`, expose the ~110 pages under speakeasy.com/docs/ai-control-plane: the first returns the page index (built from the docs sitemap and cached hourly), the second returns one page's markdown along with its public permalink to cite. `platform_get_doc` only serves paths present in the index, so it reads documentation rather than acting as a general-purpose fetcher.
