# @gram-ai/create-function

> [!NOTE]
> This scaffolder is superseded by `speakeasy functions init`, which creates
> the same projects. Install the Speakeasy AI Control Plane CLI with
> `brew install speakeasy-api/tap/cli` or `npm i -g @speakeasy-api/cli`, then
> run `speakeasy functions init my-tools`.

Scaffold a new [Speakeasy Functions](../functions/README.md) project:

```bash
npm create @gram-ai/function@latest
```

New projects depend on `@speakeasy-api/functions`.

The project templates live in
[`cli/internal/functions/templates`](../../cli/internal/functions/templates)
and are copied into this package when it is built.
