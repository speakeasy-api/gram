package proxy

import (
	"context"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
)

type countingServedInterceptor struct{ calls *int }

func (countingServedInterceptor) Name() string { return "counting" }

func (i countingServedInterceptor) InterceptRequestServed(context.Context) { *i.calls++ }

func TestRunRequestServedInterceptors_OnlyARelayedAnswerToTheRequest(t *testing.T) {
	t.Parallel()

	id, err := jsonrpc.MakeID(float64(7))
	require.NoError(t, err)
	other, err := jsonrpc.MakeID(float64(8))
	require.NoError(t, err)
	answer := &jsonrpc.Response{ID: id}

	for _, tc := range []struct {
		name   string
		status int
		msg    jsonrpc.Message
		id     jsonrpc.ID
		want   int
	}{
		{name: "answered", status: http.StatusOK, msg: answer, id: id, want: 1},
		{name: "error status", status: http.StatusInternalServerError, msg: answer, id: id, want: 0},
		{name: "another request's answer", status: http.StatusOK, msg: &jsonrpc.Response{ID: other}, id: id, want: 0},
		{name: "notification", status: http.StatusOK, msg: answer, id: jsonrpc.ID{}, want: 0},
		{name: "no message", status: http.StatusOK, msg: nil, id: id, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			p := &Proxy{RequestServedInterceptors: []RequestServedInterceptor{countingServedInterceptor{calls: &calls}}}
			p.runRequestServedInterceptors(t.Context(), tc.status, tc.msg, tc.id)
			require.Equal(t, tc.want, calls)
		})
	}
}
