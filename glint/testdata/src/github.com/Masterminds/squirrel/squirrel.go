package squirrel

type Sqlizer interface {
	ToSql() (string, []interface{}, error)
}

type SelectBuilder struct{}

func Select(columns ...string) SelectBuilder                                           { return SelectBuilder{} }
func (b SelectBuilder) Where(pred interface{}, args ...interface{}) SelectBuilder      { return b }
func (b SelectBuilder) Having(pred interface{}, args ...interface{}) SelectBuilder     { return b }
func (b SelectBuilder) GroupBy(groupBys ...string) SelectBuilder                       { return b }
func (b SelectBuilder) JoinClause(pred interface{}, args ...interface{}) SelectBuilder { return b }
func (b SelectBuilder) Column(column interface{}, args ...interface{}) SelectBuilder   { return b }

type Eq map[string]interface{}

func (e Eq) ToSql() (string, []interface{}, error) { return "", nil, nil }

type NotEq map[string]interface{}

func (e NotEq) ToSql() (string, []interface{}, error) { return "", nil, nil }

type expr struct{}

func (expr) ToSql() (string, []interface{}, error) { return "", nil, nil }

func Expr(sql string, args ...interface{}) Sqlizer { return expr{} }
