package authz

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOption_EqualsFailsOnAnAbsentSide(t *testing.T) {
	t.Parallel()

	equal, err := Some("a").Equals(Some("a"))
	require.NoError(t, err)
	require.True(t, equal)

	equal, err = Some("a").Equals(Some("b"))
	require.NoError(t, err)
	require.False(t, equal)

	for name, pair := range map[string][2]Option[string]{
		"left absent":  {None[string](), Some("a")},
		"right absent": {Some("a"), None[string]()},
		"both absent":  {None[string](), None[string]()},
		"zero value":   {{}, Some("")},
	} {
		equal, err := pair[0].Equals(pair[1])
		require.ErrorIs(t, err, ErrEmptyValue, name)
		require.False(t, equal, name)
	}
}

func TestOption_GetReportsAbsence(t *testing.T) {
	t.Parallel()

	value, ok := Some(3).Get()
	require.True(t, ok)
	require.Equal(t, 3, value)

	_, ok = None[int]().Get()
	require.False(t, ok)
}

func TestIDs_EqualsFailsOnAnAbsentID(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	equal, err := ProjectIDOf(id).Equals(ProjectIDFrom(id.String()))
	require.NoError(t, err)
	require.True(t, equal)

	equal, err = ProjectIDOf(id).Equals(ProjectIDOf(uuid.New()))
	require.NoError(t, err)
	require.False(t, equal)

	_, err = ProjectIDOf(id).Equals(ProjectID{})
	require.ErrorIs(t, err, ErrEmptyValue)
	_, err = ProjectIDOf(uuid.Nil).Equals(ProjectIDOf(uuid.Nil))
	require.ErrorIs(t, err, ErrEmptyValue)

	require.Empty(t, ProjectIDOf(uuid.Nil).String())
	require.Equal(t, id.String(), ProjectIDOf(id).String())
}

func TestIDs_StringsThatCannotNameAResourceAreAbsent(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "*", "$represented_user", "project 1", "project\t1", "project 1", "project 1"} {
		require.Empty(t, ProjectIDFrom(raw).String(), "%q", raw)
	}
	require.Equal(t, "project-1", ProjectIDFrom("project-1").String())
}
