---
cwd: ../..
shell: bash
---

# Adding and Modifying Endpoints

We use [Goa](https://github.com/goadesign/goa) to generate our API. Goa provides thin abstractions over HTTP and JSON, allowing us to define our API in Go code and generate server code automatically. This allows us to focus on the business logic of our API, rather than the HTTP details.

The overall process is:

1. Define or modify endpoints in the appropriate design file in [`server/design/`](../../server/design/)
2. Generate Goa code to update the API types and HTTP handlers
3. Implement the business logic in the corresponding service implementation
4. Test your changes and create a pull request

## Step 1. Choose or create a design file

Each service has its own design file in [`server/design/`](../../server/design/). For example:

- `environments/design.go` - Environment management endpoints
- `keys/design.go` - API key management endpoints
- `projects/design.go` - Project management endpoints
- `toolsets/design.go` - Toolset management endpoints

If you're adding a new service, create a new directory under `server/design/` with a `design.go` file.

## Step 2. Define your endpoint

Add a new `Method` to the appropriate service. Here's the basic structure:

```go
var _ = Service("resource", func() {
    Description("Manages resources in Gram.")
	
    // these are shared between all endpoints in this service
    Security(security.ByKey, func() {
        Scope("producer")
    })
	
    // these are shared between all endpoints in this service
    Security(security.Session)
    shared.DeclareErrorResponses()
	
    Method("methodName", func() {
        Description("Brief description of what this endpoint does")

        // These are specific to this endpoint
        Security(security.Session, security.ProjectSlug)
		
        Payload(UploadLogoForm)
        Result(UploadLogoResult)
    
        Result(ReturnType) // Define what the endpoint returns
    
        HTTP(func() {
            POST("/rpc/service.methodName") // or GET, PUT, DELETE
            // Add security headers as necessary
            security.SessionHeader()
            security.ProjectHeader()
        })
    
        // OpenAPI metadata for SDK generation
        Meta("openapi:operationId", "methodResourceName") // this is global to the OpenAPI spec
        Meta("openapi:extension:x-speakeasy-name-override", "methodName") // this is local to the resource
        Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "methodResourceName"}`)
    })
    
    var UpdateResourceForm = Type("UpdateResourceForm", func() {
        Required("resource_id")
        // Add security payloads as necessary
        security.SessionPayload()
        security.ProjectPayload()
        
        Attribute("resource_id", String, "The ID of the resource to update")
        Attribute("name", String, "The new name of the resource")
        Attribute("description", String, "The new description of the resource")
    })
    
    var UpdateResourceResult = Type("UpdateResourceResult", func() {
        Required("resource")
        
        // shared.Resource is a defined type in `server/design/shared/`
        Attribute("resource", shared.Resource, "The updated project with the new logo")
    })
})
```

### Common patterns:

**For endpoints that require auth in the dashboard:**

```go
Security(security.Session, security.ProjectSlug)
```

**For endpoints that require API key auth:**

```go
Security(security.ByKey, func() {
    Scope("producer") // or other scopes; we'll go over scopes later 
}, security.ProjectSlug)
```

**For endpoints with path parameters:**

```go
HTTP(func() {
    Param("id") // This becomes a query parameter e.g.: /rpc/service.method?id=foo
    GET("/rpc/service.methodName")
})
```

**For endpoints with request bodies:**

```go
Payload(func() {
    Extend(CreateResourceForm) // References a defined type
    security.SessionPayload()
})
```

> [!TIP]
>
> All APIs should take one of:
> - The api key header (Gram-Key) + project slug header (Gram-Project)
> - The session cookie (Cookie) + project slug header (Gram-Project)
> When designing new APIs, consider whether the API should be public or private, and use the appropriate security scheme in the design. This can done in the `Service` or `Method` dsl context:

## Step 3. Define types

If your endpoint needs custom types (request and response), they are defined in the same design file:

```go
var UpdateResourceForm = Type("UpdateResourceForm", func() {
    Required("resource_id")
    // Add security payloads as necessary
    security.SessionPayload()
    security.ProjectPayload()
    
    Attribute("resource_id", String, "The ID of the resource to update")
    Attribute("name", String, "The new name of the resource")
    Attribute("description", String, "The new description of the resource")
})

var UpdateResourceResult = Type("UpdateResourceResult", func() {
    Required("resource")
    
    // shared.Resource is a defined type in `server/design/shared/`
    Attribute("resource", shared.Resource, "The updated project with the new logo")
})
```

## Step 4. Generate Goa code

After defining your endpoints, generate the Goa code:

```bash
mise run gen:goa-server
```

This will update the generated code in [`server/gen/`](../../server/gen/) including:

- HTTP handlers in `gen/http/`
- Type definitions in `gen/`
- OpenAPI specification in `gen/http/openapi3.yaml`

## Step 5. Implement the business logic

Find the corresponding implementation file in [`server/internal/`](../../server/internal/) and implement your endpoint logic. For example, if you added a method to the `environments` service, implement it in `server/internal/environments/impl.go`.

The generated code will provide you with the method signature to implement:

```go
func (s *Service) MethodName(ctx context.Context, p *environments.MethodNamePayload) (*environments.ReturnType, error) {
    // Your implementation here
    return &environments.ReturnType{
        // Return your data
    }, nil
}
```

## Step 6. Test your changes

Start the server locally to test your new endpoint:

```bash
./zero
```

You can test your endpoint using:

- HTTP requests to your endpoint using cURL or Postman/Insomnia
- The generated client SDK in the dashboard (see below)

## Step 7. Update the Client SDK

To make your changes to be available in the client SDK, regenerate it. This assumes you have a valid speakeasy API key in your environment.

```bash
mise run gen:sdk
```

This will update the TypeScript SDK in [`client/sdk/`](../../client/sdk/) so it can be used in the dashboard.

## Step 8. Create a PR with your changes

Once you're happy with your changes, create a pull request. Make sure to:

1. Include tests for your new endpoint, tests live in `server/internal/{"service_name"}/endpointname_test.go`
2. Run server tests using `mise run test:server`
3. Lint your code using `mise run lint`, runs all linting tasks (client, server and migrations)
4. Test that the SDK generation works correctly

> [!TIP]
>
> You can run `mise run gen:server` to regenerate all server code (both SQLc and Goa) at once.

> [!WARNING]
>
> Never edit files in the `server/gen/` directory directly - they are generated and will be overwritten. Always make changes in the `server/design/` files instead.

## Common gotchas

- **Method names**: Use camelCase for method names (e.g., `createEnvironment`, not `create_environment`)
- **HTTP paths**: Follow the pattern `/rpc/service.methodName` for consistency.
- **Security**: Make sure to include appropriate security declarations for private endpoints
- **Types**: Define reusable types in the `server/design/shared/` rather than inline.
- **Required fields**: Use `Required()` to specify which fields are mandatory.
- **Validation**: Add validation rules like `MinLength()`, `MaxLength()`, etc. to your attributes.

## Security Scopes
#### Consumer Scope
- Purpose: Read-only and limited modification access
- Capabilities:
  - Can query/modify toolsets
  - Access MCP servers
  - Render templates
  - Get instances and templates

### Producer Scope
- Purpose: Full administrative access
- Capabilities: Everything a consumer can do, plus:
  - Upload OpenAPI documents 
  - Trigger deployments 
  - Create/update/delete projects, packages, templates 
  - Publish package versions 
  - Manage project assets