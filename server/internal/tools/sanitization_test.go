package tools_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/tools"
	"github.com/stretchr/testify/assert"
)

func Test_SanitizeName(t *testing.T) {
	t.Parallel()

	type args struct {
		name string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "succeeds with valid name",
			args: args{
				name: "some_name",
			},
			want: "some_name",
		},
		{
			name: "succeeds with name with spaces",
			args: args{
				name: "some name",
			},
			want: "some_name",
		},
		{
			name: "succeeds with name with emojis",
			args: args{
				name: "🚀 some_name",
			},
			want: "_some_name",
		},
		{
			name: "succeeds handling multiple emojis",
			args: args{
				name: "🚀 some👩🏼‍❤️‍💋_name👩🏼‍❤️‍💋‍👨🏼",
			},
			want: "_some_name_",
		},
		{
			name: "succeeds with diacritics",
			args: args{
				name: "À some_name",
			},
			want: "a_some_name",
		},
		{
			name: "succeeds with non-ascii characters in prefix",
			args: args{
				name: "≠some_name",
			},
			want: "_some_name",
		},
		{
			name: "retains ascii symbols in prefix",
			args: args{
				name: "<>some_name",
			},
			want: "_some_name",
		},
		{
			name: "succeeds in not repeating underscores",
			args: args{
				name: "some[_name",
			},
			want: "some_name",
		},
		{
			name: "succeeds replacing prefix numbers",
			args: args{
				name: "1some_name",
			},
			want: "1some_name",
		},
		{
			name: "retains prefix numbers with following symbols",
			args: args{
				name: "1>some_name",
			},
			want: "1_some_name",
		},
		{
			name: "retains multiple prefix numbers with following symbols",
			args: args{
				name: "10>some_name",
			},
			want: "10_some_name",
		},
		{
			name: "succeeds handling trailing numbers",
			args: args{
				name: "some_name10",
			},
			want: "some_name10",
		},
		{
			name: "succeeds with single underscores",
			args: args{
				name: "some_string_with_underscores",
			},
			want: "some_string_with_underscores",
		},
		{
			name: "succeeds with double underscores",
			args: args{
				name: "some__string_with_underscores",
			},
			want: "some_string_with_underscores",
		},
		{
			name: "succeeds with many underscores",
			args: args{
				name: "some___string__with_underscores__",
			},
			want: "some_string_with_underscores_",
		},
		{
			name: "succeeds with leading and trailing underscores",
			args: args{
				name: "_operation_with_leading_and_trailing_underscores_",
			},
			want: "_operation_with_leading_and_trailing_underscores_",
		},
		{
			name: "succeeds with multiple leading and trailing underscores",
			args: args{
				name: "__operation_with_multiple_leading_and_trailing_underscores__",
			},
			want: "_operation_with_multiple_leading_and_trailing_underscores_",
		},
		{
			name: "succeeds with equals sign",
			args: args{
				name: "_sap_sales_get_a_sales_order_item_sales_order=_sales_8408405e",
			},
			want: "_sap_sales_get_a_sales_order_item_sales_order_sales_8408405e",
		},
		// Additional tests based on SlugPatternRE: ^[a-z0-9_-]{1,128}$
		{
			name: "converts uppercase to lowercase",
			args: args{
				name: "SomeUpperCaseName",
			},
			want: "someuppercasename",
		},
		{
			name: "handles mixed case with symbols",
			args: args{
				name: "Mixed_Case-Name123",
			},
			want: "mixed_case-name123",
		},
		{
			name: "removes invalid ASCII symbols",
			args: args{
				name: "name@with#symbols$",
			},
			want: "name_with_symbols_",
		},
		{
			name: "handles parentheses and brackets",
			args: args{
				name: "function(param)[index]",
			},
			want: "function_param_index_",
		},
		{
			name: "handles dots and colons",
			args: args{
				name: "namespace.class:method",
			},
			want: "namespace_class_method",
		},
		{
			name: "handles complex symbols",
			args: args{
				name: "api/v1/{id}/status",
			},
			want: "api_v1_id_status",
		},
		{
			name: "preserves valid hyphens",
			args: args{
				name: "kebab-case-name",
			},
			want: "kebab-case-name",
		},
		{
			name: "preserves mixed hyphens and underscores",
			args: args{
				name: "my-api_get_users",
			},
			want: "my-api_get_users",
		},
		{
			name: "preserves hyphens with uppercase conversion",
			args: args{
				name: "My-API-Endpoint",
			},
			want: "my-api-endpoint",
		},
		{
			name: "handles leading hyphen",
			args: args{
				name: "-leading-hyphen",
			},
			want: "-leading-hyphen",
		},
		{
			name: "handles trailing hyphen",
			args: args{
				name: "trailing-hyphen-",
			},
			want: "trailing-hyphen-",
		},
		{
			name: "handles only symbols",
			args: args{
				name: "@#$%^&*()",
			},
			want: "_", // Should be single underscore since all symbols become underscores
		},
		{
			name: "handles leading symbols with text",
			args: args{
				name: "@#$name",
			},
			want: "_name", // Leading symbols become underscore, then name
		},
		{
			name: "handles trailing symbols",
			args: args{
				name: "name@#$",
			},
			want: "name_",
		},
		{
			name: "preserves numbers at start",
			args: args{
				name: "123_name",
			},
			want: "123_name",
		},
		{
			name: "handles consecutive invalid chars",
			args: args{
				name: "name@@@test",
			},
			want: "name_test",
		},
		{
			name: "handles slash and backslash",
			args: args{
				name: "path/to\\resource",
			},
			want: "path_to_resource",
		},
		{
			name: "handles question mark and exclamation",
			args: args{
				name: "what?!happening",
			},
			want: "what_happening",
		},
		{
			name: "handles percent and plus",
			args: args{
				name: "score+100%",
			},
			want: "score_100_",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tools.SanitizeName(tt.args.name)
			assert.Equal(t, tt.want, got)
		})
	}
}
