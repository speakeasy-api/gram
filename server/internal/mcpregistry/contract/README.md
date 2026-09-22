# Registry record contract

Source: `speakeasy-api/mcp-registry` revision
`c5873aaaad0a2988e39f1ed7c237006bbfb05826`,
`packages/registry-api/src/Schema.ts`, `ServerResponse`.

`record.schema.json` is the Effect `4.0.0-rc.115`
`Schema.toJsonSchemaDocument(ServerResponse).schema` export, with an explicit
2020-12 dialect. There were no external references or definitions. The only
semantic adaptation is renaming `uri` and `date-time` formats to
`gram-source-uri` and `gram-source-date-time`. Register the adapters before
validation: generic validators without them are not conformant. The URI regex
remains in the schema; its adapter adds the source's IP-literal check. Timestamp
adapters preserve the source's calendar and UTC month-end leap-second rules
without consulting a mutable leap-second table.

Go callers use `Compile`; browser callers pass their imported `Validator` and
`format` registry to `compile` in `formats.mjs`. Both reject unknown formats at
initialization. These minimal adapters were brought forward from Task 2 because
Task 0 must verify both validators; no application service is implemented here.

From the repository root:

```sh
mise exec -- go test ./server/internal/mcpregistry/contract
mise exec -- aube run -F dashboard test:registry-contract
```

The JS harness uses the existing dashboard dependency through its supported
`test:registry-contract` package script, not a standalone-registry dependency.
`metaschemas.json` bundles the standard 2020-12 root and vocabulary schemas from
`github.com/santhosh-tekuri/jsonschema/v6@v6.0.2/metaschemas/draft/2020-12`
(their original `$id` values identify each standard resource). Browser compilation
validates the schema itself against these offline resources before use.

## Reproduce source parity

Supply an explicit checkout/archive of the pinned revision with its pinned
Effect 4.0.0-rc.115 dependency installed using `aube` (outside Gram). Do not run
Pulse acquisition scripts. Then run:

```sh
mise exec -- node server/internal/mcpregistry/contract/check-source.mjs /path/to/pinned-source
```

This checks the schema-source SHA-256, compares the canonical export structurally,
checks all shared fixtures against the source decoder, and compares the two
starter records to their source hashes. The export uses the source's model, not
a second handwritten schema.

The baseline is only the user-approved Vercel/Linear starter, not the historical
62-record source inventory or Stage B cutover evidence. Exact raw record bytes
are excluded from oxfmt via the repository's existing ignore convention; their
manifest hashes and validation remain tested. Public review found no credentials,
email addresses, account identifiers, or customer account-context payloads in
these two records; tool descriptions use illustrative examples and metadata
contains public vendor discovery/authentication declarations. No records were
redacted, and the other 60 source records were not copied.
