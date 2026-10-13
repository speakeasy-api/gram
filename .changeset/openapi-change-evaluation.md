---
"server": minor
"dashboard": minor
---

Uploading a new version of an OpenAPI source now starts with a review of what it changes for your MCP servers. The review lists breaking changes and changes that need a look, such as removed tools, new required inputs, and new or removed credentials. It also shows which MCP servers use the source and how many of their tools are affected, all before anything deploys. Two new deployment endpoints back this: `deployments.evaluateOpenAPIv3` compares an uploaded document with the deployed version without deploying it, and `deployments.evaluation` compares a deployment with the one before it.
