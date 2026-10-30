package deployments

import (
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"reflect"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/stretchr/testify/require"
)

func TestNewServiceRequiresExplicitCatalog(t *testing.T) {
	t.Parallel()
	signature := reflect.TypeOf(NewService)
	require.False(t, signature.IsVariadic(), "catalog must be a required dependency, not an empty fallback")
	require.Equal(t, reflect.TypeFor[*externalmcp.CatalogService](), signature.In(signature.NumIn()-1))
}

func TestNewServiceKeepsNativeCatalogReader(t *testing.T) {
	t.Parallel()
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	reader := externalmcp.NewNativeRegistryReader(mcpregistry.New(nil, validator))
	catalog := externalmcp.NewCatalogService(nil, reader)
	service := NewService(testenv.NewLogger(t), testenv.NewTracerProvider(t), nil, nil, &sessions.Manager{}, nil, nil, nil, nil, nil, catalog)
	require.Same(t, catalog, service.catalog)
	resolved, err := service.catalog.ReaderFor(externalmcp.CatalogSource{Registry: externalmcp.Registry{ID: externalmcp.NativeCatalogRegistryID}, SourceType: "native_v1", AuthProfile: "none"})
	require.NoError(t, err)
	require.Same(t, reader, resolved)
}
