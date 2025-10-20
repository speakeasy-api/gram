---
"@gram/cli": minor
---

Introducing two new commands to the Gram CLI:

```
gram stage openapi --slug <slug> --location <path>
gram stage function --slug <slug> --location <path>
```

These commands can be used to gradually build out deployment configs by
adding OpenAPI documents and Gram Functions zip files as sources. After
all sources are added, `gram push` can be used to deploy the staged
configuration.

In practice, this should make it easier to script a Gram deployment in CI/CD and
locally compared to authoring a full deployment JSON config manually.
