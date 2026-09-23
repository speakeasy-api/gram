# Registry record contract

The Gram registry validates records against the checked-in `record.schema.json`
and its Go/browser format adapters. This contract originated from
`speakeasy-api/mcp-registry` revision
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
initialization. Shared conformance fixtures exercise the checked-in contract in
both implementations; normal development does not require the original repository.

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

## Provenance and baseline

The source revision above records the contract's provenance, not a dependency
on an external checkout. The checked-in schema, adapters and conformance tests
define the current Gram contract; these tests do not rerun the original source
decoder or establish ongoing parity with changes in that repository.

The baseline is only the user-approved Vercel/Linear starter, not the historical
62-record source inventory or Stage B cutover evidence. Exact raw record bytes
are excluded from oxfmt via the repository's existing ignore convention; their
manifest hashes and validation remain tested. Public review found no credentials,
email addresses, account identifiers, or customer account-context payloads in
these two records; tool descriptions use illustrative examples and metadata
contains public vendor discovery/authentication declarations. No records were
redacted, and the other 60 source records were not copied.

Identity is the exact, case-sensitive `server.name` string for create, import,
lookup, uniqueness and immutability checks. Do not lowercase approved names;
case-insensitive search does not change identity semantics.

`TestMetaschemaIntegrity` pins the reviewed bundle bytes. Its parsed resources
were compared against all nine resources in the pinned Go module directory;
when deliberately updating the bundle, repeat that comparison before updating
the digest.
