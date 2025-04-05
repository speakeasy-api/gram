package tools_test

import (
	"testing"

	"github.com/speakeasy-api/gram/internal/tools"
	"github.com/stretchr/testify/assert"
)

func Test_SanitizeName_Success(t *testing.T) {
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
			want: "some_name",
		},
		{
			name: "succeeds handling multiple emojis",
			args: args{
				name: "🚀 some👩🏼‍❤️‍💋_name👩🏼‍❤️‍💋‍👨🏼",
			},
			want: "some_name_",
		},
		{
			name: "succeeds with diacritics",
			args: args{
				name: "À some_name",
			},
			want: "A_some_name",
		},
		{
			name: "succeeds with non-ascii characters in prefix",
			args: args{
				name: "≠some_name",
			},
			want: "some_name",
		},
		{
			name: "retains ascii symbols in prefix",
			args: args{
				name: "<>some_name",
			},
			want: "<>some_name",
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
			want: "1>some_name",
		},
		{
			name: "retains multiple prefix numbers with following symbols",
			args: args{
				name: "10>some_name",
			},
			want: "10>some_name",
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tools.SanitizeName(tt.args.name)
			assert.Equal(t, tt.want, got)
		})
	}
}
