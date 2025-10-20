---
"@gram/cli": minor
---

Added a `--method replace|merge` flag to the `gram push` command. This flag
allows users to specify whether a push should replace all previous deployment
artifacts or merge on top of them. The default behavior is `--method merge`. As
an illustrative example:

**With `--method replace`:**

```
T0:
  Current project artifacts:
    - petstore.openapi.yaml
    - greet.zip

T1:
  User runs:
    gram stage function --slug ecommerce --location ecommerce.zip
    gram push --method replace

T2:
  Resulting project artifacts:
    - ecommerce (ecommerce.zip)
```

**With `--method merge` (the new default behavior):**

```
T0:
  Current project artifacts:
    - petstore (petstore.openapi.yaml)
    - greeter (greet.zip)

T1:
  User runs:
    gram stage function --slug ecommerce --location ecommerce.zip
    gram push --method merge

T2:
  Resulting project artifacts:
    - petstore (petstore.openapi.yaml)
    - greeter (greet.zip)
    - ecommerce (ecommerce.zip)
```
