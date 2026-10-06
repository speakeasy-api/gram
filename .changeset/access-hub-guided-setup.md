---
"server": minor
"dashboard": minor
---

The Access Hub catalog now offers Claude Tag. Its card opens a page listing the access allowed under the platform, and **Register new access** opens a guided setup that asks only for the Anthropic organization ID, an agent and optional tags, then trusts the platform and allows every channel in that organization in one step. Catalog platforms and their setup steps are YAML files shipped with the server, validated when loaded, and served by the new `workloadIdentities.listPlatforms` method, which requires `workload:read`.

The Claude Tag setup links to Claude Tag's federated access settings and to where the Anthropic organization ID is found, can create an agent inline (named Claude Tag by default), and walks through the two Anthropic console dialogs as a checklist: each console field with the value to paste or what to do, and a checkbox to tick off as the operator goes. Definitions place these rows with the new `checklist_item` block. The setup no longer warns that the access rule admits more than one identity, since the operator cannot change that for a catalog platform.
