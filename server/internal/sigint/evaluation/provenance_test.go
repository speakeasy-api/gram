package evaluation

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/server/internal/classifier"
)

func TestReadingIdentityPresence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		actor   string
		billing string
	}{
		{name: "unknown"},
		{name: "actor does not imply billing", actor: "message-user"},
		{name: "billing does not imply actor", billing: "owner-user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := message()
			m.SetRole(conversationv1.Message_ROLE_ASSISTANT)
			p := &conversationv1.Message_Provenance{}
			if tc.actor != "" {
				p.SetUserId(tc.actor)
			}
			if tc.billing != "" {
				p.SetBillingUserId(tc.billing)
			}
			m.SetProvenance(p)
			sensor, ready := compileSensor(sensors()[0])
			require.True(t, ready)
			answers := map[classifier.QuestionKey]classifier.QuestionOutcome{}
			for _, q := range sensor.questions {
				answers[q.Key] = classifier.QuestionOutcome{Key: q.Key, Answer: &classifier.Answer{Noul: &classifier.NoulAnswer{Probability: 0.5}}}
			}
			r, err := reading(m, sensor, answers, "attempt", "time", classifier.Result{})
			require.NoError(t, err)
			data, err := proto.Marshal(r)
			require.NoError(t, err)
			decoded := &sigintv1.Reading{}
			require.NoError(t, proto.Unmarshal(data, decoded))
			require.Equal(t, tc.actor != "", decoded.HasActor())
			require.Equal(t, tc.actor, decoded.GetActor().GetUserId())
			require.Equal(t, tc.billing != "", decoded.HasBillingUserId())
			require.Equal(t, tc.billing, decoded.GetBillingUserId())
			require.False(t, decoded.HasAccount())
			require.False(t, decoded.HasSource())
			require.False(t, decoded.HasReplayed())
		})
	}
}
