package emaildomain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/emaildomain"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		email string
		want  emaildomain.Class
	}{
		{name: "free webmail", email: "jane@gmail.com", want: emaildomain.FreeWebmail},
		{name: "googlemail", email: "jane@googlemail.com", want: emaildomain.FreeWebmail},
		{name: "disposable", email: "jane@mailinator.com", want: emaildomain.Disposable},
		{name: "regional yahoo", email: "jane@yahoo.co.uk", want: emaildomain.FreeWebmail},
		{name: "regional hotmail", email: "jane@hotmail.fr", want: emaildomain.FreeWebmail},
		{name: "regional outlook", email: "jane@outlook.com.br", want: emaildomain.FreeWebmail},
		{name: "regional live", email: "jane@live.co.uk", want: emaildomain.FreeWebmail},
		{name: "regional gmx", email: "jane@gmx.at", want: emaildomain.FreeWebmail},
		{name: "regional yandex", email: "jane@yandex.com.tr", want: emaildomain.FreeWebmail},
		{name: "regional zoho", email: "jane@zohomail.eu", want: emaildomain.FreeWebmail},
		{name: "german provider", email: "jane@web.de", want: emaildomain.FreeWebmail},
		{name: "french provider", email: "jane@orange.fr", want: emaildomain.FreeWebmail},
		{name: "italian provider", email: "jane@libero.it", want: emaildomain.FreeWebmail},
		{name: "chinese provider", email: "jane@foxmail.com", want: emaildomain.FreeWebmail},
		{name: "korean provider", email: "jane@naver.com", want: emaildomain.FreeWebmail},
		{name: "indian provider", email: "jane@rediffmail.com", want: emaildomain.FreeWebmail},
		{name: "mail.ru alias domain", email: "jane@bk.ru", want: emaildomain.FreeWebmail},
		{name: "consumer internet provider", email: "jane@comcast.net", want: emaildomain.FreeWebmail},
		{name: "provider name under an unlisted suffix", email: "jane@yahoo.example", want: emaildomain.Business},
		{name: "business", email: "jane@acme.com", want: emaildomain.Business},
		{name: "empty", email: "", want: emaildomain.Unknown},
		{name: "no at sign", email: "jane", want: emaildomain.Unknown},
		{name: "nothing after at sign", email: "jane@", want: emaildomain.Unknown},
		{name: "only a dot after at sign", email: "jane@.", want: emaildomain.Unknown},
		{name: "uppercase and whitespace", email: "  Jane@GMAIL.COM ", want: emaildomain.FreeWebmail},
		{name: "trailing dot", email: "jane@yopmail.com.", want: emaildomain.Disposable},
		{name: "last at sign wins", email: "jane@acme.com@gmail.com", want: emaildomain.FreeWebmail},
		{name: "subdomain of free webmail", email: "jane@eu.gmail.com", want: emaildomain.Business},
		{name: "subdomain of disposable", email: "jane@x.mailinator.com", want: emaildomain.Business},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, emaildomain.Classify(tc.email))
		})
	}
}

func TestDomain(t *testing.T) {
	t.Parallel()

	require.Equal(t, "acme.com", emaildomain.Domain(" Jane@ACME.com. "))
	require.Empty(t, emaildomain.Domain("jane"))
}
