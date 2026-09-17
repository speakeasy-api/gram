package oktaapplications

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func app(id string) Application {
	return Application{ID: id, Label: id, Name: "oidc_client", SignOnMode: "OPENID_CONNECT", Status: "ACTIVE", Features: nil}
}

func user(appID, userID string) Assignment {
	return Assignment{AppID: appID, Kind: PrincipalKindUser, PrincipalID: userID, Scope: "USER"}
}

func group(appID, groupID string) Assignment {
	return Assignment{AppID: appID, Kind: PrincipalKindGroup, PrincipalID: groupID, Scope: ""}
}

func key(a Assignment) AssignmentKey { return a.key() }

func TestReconcile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		liveApps        []string
		liveAssignments []AssignmentKey
		snap            Snapshot
		want            Diff
	}{
		{
			name:     "first run adds everything",
			liveApps: nil,
			snap:     Snapshot{Applications: []Application{app("a"), app("b")}, Assignments: []Assignment{user("a", "u1"), group("b", "g1")}},
			want:     Diff{AddedApplications: []string{"a", "b"}, AddedAssignments: []AssignmentKey{key(user("a", "u1")), key(group("b", "g1"))}},
		},
		{
			name:            "unchanged run is empty",
			liveApps:        []string{"a"},
			liveAssignments: []AssignmentKey{key(user("a", "u1"))},
			snap:            Snapshot{Applications: []Application{app("a")}, Assignments: []Assignment{user("a", "u1")}},
			want:            Diff{},
		},
		{
			name:            "removed app removes its assignments",
			liveApps:        []string{"a", "b"},
			liveAssignments: []AssignmentKey{key(user("a", "u1")), key(user("b", "u2"))},
			snap:            Snapshot{Applications: []Application{app("a")}, Assignments: []Assignment{user("a", "u1")}},
			want:            Diff{RemovedApplications: []string{"b"}, RemovedAssignments: []AssignmentKey{key(user("b", "u2"))}},
		},
		{
			name:            "reassignment from user to group",
			liveApps:        []string{"a"},
			liveAssignments: []AssignmentKey{key(user("a", "u1"))},
			snap:            Snapshot{Applications: []Application{app("a")}, Assignments: []Assignment{group("a", "g1")}},
			want:            Diff{AddedAssignments: []AssignmentKey{key(group("a", "g1"))}, RemovedAssignments: []AssignmentKey{key(user("a", "u1"))}},
		},
		{
			name:            "truncated application listing removes nothing",
			liveApps:        []string{"a", "b"},
			liveAssignments: []AssignmentKey{key(user("b", "u2"))},
			snap:            Snapshot{Applications: []Application{app("a")}, ApplicationsTruncated: true},
			want:            Diff{},
		},
		{
			name:            "incomplete assignment listing keeps that app's assignments",
			liveApps:        []string{"a", "b"},
			liveAssignments: []AssignmentKey{key(user("a", "u1")), key(user("b", "u2"))},
			snap: Snapshot{
				Applications:          []Application{app("a"), app("b")},
				Assignments:           nil,
				IncompleteAssignments: map[string]bool{"a": true},
			},
			want: Diff{RemovedAssignments: []AssignmentKey{key(user("b", "u2"))}},
		},
		{
			name:            "revived app is an add",
			liveApps:        []string{},
			liveAssignments: nil,
			snap:            Snapshot{Applications: []Application{app("a")}},
			want:            Diff{AddedApplications: []string{"a"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Reconcile(tt.liveApps, tt.liveAssignments, tt.snap)
			require.Equal(t, tt.want.AddedApplications, got.AddedApplications)
			require.Equal(t, tt.want.RemovedApplications, got.RemovedApplications)
			require.Equal(t, tt.want.AddedAssignments, got.AddedAssignments)
			require.Equal(t, tt.want.RemovedAssignments, got.RemovedAssignments)
		})
	}
}

func TestIsInternalApplication(t *testing.T) {
	t.Parallel()

	require.True(t, IsInternalApplication("okta_enduser", "OPENID_CONNECT"))
	require.True(t, IsInternalApplication("saasure", ""))
	require.False(t, IsInternalApplication("oidc_client", "OPENID_CONNECT"))
	// Labels never match; only template names do.
	require.False(t, IsInternalApplication("Okta Dashboard", "OPENID_CONNECT"))
	require.False(t, IsInternalApplication("okta_enduser_custom", "OPENID_CONNECT"))
}

func TestSnapshotTruncated(t *testing.T) {
	t.Parallel()

	require.False(t, Snapshot{}.Truncated())
	require.True(t, Snapshot{ApplicationsTruncated: true}.Truncated())
	require.True(t, Snapshot{IncompleteAssignments: map[string]bool{"a": true}}.Truncated())
}
