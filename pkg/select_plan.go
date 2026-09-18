package pkg

import (
	"fmt"
	"github.com/xwb1989/sqlparser"
	"regexp"
	"strconv"
	"strings"
)

type selectPlan struct {
	table, filter string
	vectorField   string
	vectorExpr    sqlparser.Expr
	limit, offset int64
}

var fieldIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// planSelect rejects syntax whose semantics cannot be preserved by scalar Query.
func planSelect(s *sqlparser.Select) (selectPlan, error) {
	p := selectPlan{limit: 100}
	if s.Distinct != "" || len(s.GroupBy) > 0 || s.Having != nil || len(s.OrderBy) > 0 || s.Lock != "" {
		return p, fmt.Errorf("DISTINCT, GROUP BY, HAVING, ORDER BY and locking are not supported")
	}
	if len(s.From) != 1 {
		return p, fmt.Errorf("SELECT requires one collection")
	}
	a, ok := s.From[0].(*sqlparser.AliasedTableExpr)
	if !ok {
		return p, fmt.Errorf("joins are not supported")
	}
	t, ok := a.Expr.(sqlparser.TableName)
	if !ok || !a.As.IsEmpty() || a.Hints != nil || !t.Qualifier.IsEmpty() {
		return p, fmt.Errorf("subqueries, table aliases, hints and qualified tables are not supported")
	}
	p.table = t.Name.String()
	for _, e := range s.SelectExprs {
		if star, ok := e.(*sqlparser.StarExpr); ok {
			if len(s.SelectExprs) != 1 || !star.TableName.IsEmpty() {
				return p, fmt.Errorf("only an unqualified standalone * is supported")
			}
			continue
		}
		a, ok := e.(*sqlparser.AliasedExpr)
		if !ok || !a.As.IsEmpty() {
			return p, fmt.Errorf("projection aliases are not supported")
		}
		if sqlparser.String(a.Expr) == "count(*)" && len(s.SelectExprs) == 1 {
			continue
		}
		c, ok := a.Expr.(*sqlparser.ColName)
		if !ok || !c.Qualifier.IsEmpty() {
			return p, fmt.Errorf("projection must be an unqualified field")
		}
	}
	var err error
	if s.Where != nil {
		expr, field, vector, e := extractVector(s.Where.Expr)
		if e != nil {
			return p, e
		}
		p.vectorField = field
		p.vectorExpr = vector
		if expr != nil {
			p.filter, err = scalarFilter(expr)
		}
		if err != nil {
			return p, err
		}
	}
	if s.Limit != nil {
		p.limit, err = selectInteger(s.Limit.Rowcount)
		if err != nil {
			return p, err
		}
		if s.Limit.Offset != nil {
			p.offset, err = selectInteger(s.Limit.Offset)
			if err != nil {
				return p, err
			}
		}
	}
	if p.limit > 16384 || p.offset > 16384 || p.limit+p.offset > 16384 {
		return p, fmt.Errorf("LIMIT + OFFSET must not exceed 16384")
	}
	return p, nil
}
func selectInteger(e sqlparser.Expr) (int64, error) {
	v, ok := e.(*sqlparser.SQLVal)
	if !ok || v.Type != sqlparser.IntVal {
		return 0, fmt.Errorf("LIMIT and OFFSET must be non-negative integer literals")
	}
	n, err := strconv.ParseInt(string(v.Val), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid LIMIT or OFFSET")
	}
	return n, nil
}
func scalarFilter(e sqlparser.Expr) (string, error) {
	switch v := e.(type) {
	case *sqlparser.AndExpr:
		return filterPair(v.Left, v.Right, "and")
	case *sqlparser.OrExpr:
		return filterPair(v.Left, v.Right, "or")
	case *sqlparser.NotExpr:
		x, err := scalarFilter(v.Expr)
		return "not (" + x + ")", err
	case *sqlparser.ParenExpr:
		x, err := scalarFilter(v.Expr)
		return "(" + x + ")", err
	case *sqlparser.ComparisonExpr:
		op := v.Operator
		switch op {
		case "=":
			op = "=="
		case "!=", "<>":
			op = "!="
		case "in", "not in":
			l, err := filterValue(v.Left)
			if err != nil {
				return "", err
			}
			tuple, ok := v.Right.(sqlparser.ValTuple)
			if !ok {
				return "", fmt.Errorf("IN requires a literal list")
			}
			items := []string{}
			for _, x := range tuple {
				switch x.(type) {
				case *sqlparser.SQLVal, sqlparser.BoolVal, *sqlparser.UnaryExpr:
				default:
					return "", fmt.Errorf("IN requires literals")
				}
				item, err := filterValue(x)
				if err != nil {
					return "", err
				}
				items = append(items, item)
			}
			return l + " " + op + " [" + strings.Join(items, ",") + "]", nil
		case "like":
			if _, ok := v.Right.(*sqlparser.SQLVal); !ok {
				return "", fmt.Errorf("LIKE requires a string literal")
			}
		case "<", "<=", ">", ">=":
		default:
			return "", fmt.Errorf("unsupported WHERE operator %s", op)
		}
		if v.Escape != nil {
			return "", fmt.Errorf("ESCAPE is not supported")
		}
		l, err := filterValue(v.Left)
		if err != nil {
			return "", err
		}
		r, err := filterValue(v.Right)
		if err != nil {
			return "", err
		}
		return l + " " + op + " " + r, nil
	default:
		return "", fmt.Errorf("unsupported WHERE expression %T", e)
	}
}
func filterPair(l, r sqlparser.Expr, op string) (string, error) {
	a, err := scalarFilter(l)
	if err != nil {
		return "", err
	}
	b, err := scalarFilter(r)
	if err != nil {
		return "", err
	}
	return "(" + a + " " + op + " " + b + ")", nil
}
func filterValue(e sqlparser.Expr) (string, error) {
	switch v := e.(type) {
	case *sqlparser.ColName:
		if v.Qualifier.IsEmpty() && fieldIdentifier.MatchString(v.Name.String()) {
			return v.Name.String(), nil
		}
	case *sqlparser.SQLVal:
		switch v.Type {
		case sqlparser.StrVal:
			return strconv.Quote(string(v.Val)), nil
		case sqlparser.IntVal, sqlparser.FloatVal:
			return string(v.Val), nil
		}
	case sqlparser.BoolVal:
		return strconv.FormatBool(bool(v)), nil
	case *sqlparser.UnaryExpr:
		if v.Operator == "-" || v.Operator == "+" {
			if n, ok := v.Expr.(*sqlparser.SQLVal); ok && (n.Type == sqlparser.IntVal || n.Type == sqlparser.FloatVal) {
				return v.Operator + string(n.Val), nil
			}
		}
	}
	return "", fmt.Errorf("unsupported WHERE value %T", e)
}

// Vector predicates are allowed only as positive AND conjuncts. Treating ANN as
// a scalar predicate under OR/NOT would change the meaning of the SQL query.
func extractVector(e sqlparser.Expr) (sqlparser.Expr, string, sqlparser.Expr, error) {
	switch v := e.(type) {
	case *sqlparser.ParenExpr:
		x, f, q, err := extractVector(v.Expr)
		if err != nil || x == nil {
			return x, f, q, err
		}
		return &sqlparser.ParenExpr{Expr: x}, f, q, nil
	case *sqlparser.AndExpr:
		l, lf, lq, err := extractVector(v.Left)
		if err != nil {
			return nil, "", nil, err
		}
		r, rf, rq, err := extractVector(v.Right)
		if err != nil {
			return nil, "", nil, err
		}
		if lf != "" && rf != "" {
			return nil, "", nil, fmt.Errorf("only one vector predicate is supported")
		}
		if rf != "" {
			lf, lq = rf, rq
		}
		if l == nil {
			return r, lf, lq, nil
		}
		if r == nil {
			return l, lf, lq, nil
		}
		return &sqlparser.AndExpr{Left: l, Right: r}, lf, lq, nil
	case *sqlparser.ComparisonExpr:
		if v.Operator == "like" {
			if fn, ok := v.Right.(*sqlparser.FuncExpr); ok && strings.EqualFold(fn.Name.String(), "json_vector") {
				col, ok := v.Left.(*sqlparser.ColName)
				if !ok || !col.Qualifier.IsEmpty() || v.Escape != nil {
					return nil, "", nil, fmt.Errorf("invalid vector predicate")
				}
				return nil, col.Name.String(), v.Right, nil
			}
		}
	}
	return e, "", nil, nil
}
