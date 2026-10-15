package mcpregistry

import "github.com/speakeasy-api/gram/server/internal/testenv"

// Test-only bridges let the HTTP integration tests exercise public APIs without
// importing the local fixture back into the registry package.
var NewTestService = newTestService
var PublicationDate = publicationDate

func InfraForTest() *testenv.Environment { return infra }
