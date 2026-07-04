package celenv_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
)

func newEngine(t *testing.T) *celenv.Engine {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	return eng
}

func sampleActor() celenv.Actor {
	return celenv.Actor{
		Email:          "ada@acme.com",
		DepartmentName: "Engineering",
		JobTitle:       "Staff Engineer",
		EmployeeType:   "full_time",
		DivisionName:   "R&D",
		CostCenterName: "CC-1001",
		Groups:         []string{"eng-frontier", "leadership"},
	}
}

func TestCompileAndEvalExpressions(t *testing.T) {
	t.Parallel()

	eng := newEngine(t)
	actor := sampleActor()

	cases := []struct {
		expr string
		want bool
	}{
		{expr: `department_name == "Engineering"`, want: true},
		{expr: `department_name != "Engineering"`, want: false},
		{expr: `"leadership" in groups`, want: true},
		{expr: `"interns" in groups`, want: false},
		{expr: `email.endsWith("@acme.com")`, want: true},
		{expr: `email.startsWith("ada")`, want: true},
		{expr: `job_title.contains("Engineer")`, want: true},
		{expr: `job_title.matches("^Staff.*")`, want: true},
		{expr: `employee_type == "intern"`, want: false},
		{expr: `division_name == "R&D" && cost_center_name == "CC-1001"`, want: true},
		{expr: `department_name == "Design" || "eng-frontier" in groups`, want: true},
	}

	for _, tc := range cases {
		prg, err := eng.Compile(tc.expr)
		require.NoError(t, err, "compile %q", tc.expr)
		got, err := eng.Eval(prg, actor)
		require.NoError(t, err, "eval %q", tc.expr)
		require.Equal(t, tc.want, got, "eval %q", tc.expr)
	}
}

func TestEvalWithEmptyAttributes(t *testing.T) {
	t.Parallel()

	eng := newEngine(t)
	prg, err := eng.Compile(`department_name == ""`)
	require.NoError(t, err)

	got, err := eng.Eval(prg, celenv.Actor{})
	require.NoError(t, err)
	require.True(t, got, "unset attributes evaluate as empty strings")

	prg, err = eng.Compile(`"anything" in groups`)
	require.NoError(t, err)
	got, err = eng.Eval(prg, celenv.Actor{})
	require.NoError(t, err)
	require.False(t, got, "nil groups behave as an empty list")
}

func TestCompileRejectsUnknownVariables(t *testing.T) {
	t.Parallel()

	_, err := newEngine(t).Compile(`favorite_color == "blue"`)
	require.Error(t, err)
}

func TestCompileRejectsNonBooleanExpressions(t *testing.T) {
	t.Parallel()

	_, err := newEngine(t).Compile(`department_name`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must evaluate to bool")
}

func TestCompileRejectsSyntaxErrors(t *testing.T) {
	t.Parallel()

	_, err := newEngine(t).Compile(`department_name == `)
	require.Error(t, err)
}
