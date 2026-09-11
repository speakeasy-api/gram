# Admin SDK usage

The Admin application uses the private, same-origin clients in
[`src/lib/gramAdminClient.ts`](src/lib/gramAdminClient.ts). Keep generated SDK
imports and request options inside that boundary rather than exposing them to
page components.

## Image upload mutation variables

The generated `useAdminUploadPlatformImageMutation` hook takes a variables
object, **not a bare Blob**. Inside the SDK integration layer, given `mutate`
from that hook and a browser `File` or `Blob`, use:

```ts
mutate({ request: blob });
```

For example, a browser file-input handler can submit the selected file without
Node.js filesystem APIs or an asynchronous form handler:

```ts
const file = input.files?.[0];
if (file) {
  mutate({ request: file });
}
```

`File` extends `Blob`. The generated
[`AdminUploadPlatformImageMutationVariables`](src/sdk/src/react-query/adminUploadPlatformImage.ts)
type also accepts `ArrayBuffer`, `Uint8Array`, and `ReadableStream<Uint8Array>`
under `request`; `options` is optional and remains internal to the integration
layer.

### Generated documentation limitation

The mutation example in [`src/sdk/REACT_QUERY.md`](src/sdk/REACT_QUERY.md)
currently passes a bare Blob and uses `await openAsBlob(...)` inside a
non-async browser form handler. Use the wrapped browser example above instead.

The pinned Speakeasy generator (`1.796.1`) owns that file. Its bundled
`sdk-customization/readme-customization.md` guide documents custom sections in
`README.md` and `generation.additionalDocs`, but does not document a source
override for the React Query mutation template. This non-generated guide keeps
the correction durable without modifying generated output or enabling
persistent edits. The generated example still needs an upstream template fix.
