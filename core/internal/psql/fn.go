package psql

import (
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func (c *compilerContext) renderFunctionSearchRank(sel *qcode.Select, f qcode.Field) {
	c.dialect.RenderSearchRank(c, sel, f)
}

func (c *compilerContext) renderFunctionSearchHeadline(sel *qcode.Select, f qcode.Field) {
	c.dialect.RenderSearchHeadline(c, sel, f)
}

func (c *compilerContext) renderTableFunction(sel *qcode.Select) {
	c.renderFunction(sel.Table, sel.Args)
	c.alias(sel.Table)
}

func (c *compilerContext) renderFieldFunction(sel *qcode.Select, f qcode.Field) {
	switch f.Func.Name {
	case "search_rank":
		c.renderFunctionSearchRank(sel, f)
	case "search_headline":
		c.renderFunctionSearchHeadline(sel, f)
	default:
		if f.AggFilter.Exp != nil {
			c.renderAggFunctionWithFilter(sel, f)
		} else {
			c.renderFunction(f.Func.Name, f.Args)
		}
	}
}

// renderAggFunctionWithFilter renders an aggregate with its `if:` condition
// pushed inside the function call:
//
//	count_id(if: cond)         → count((CASE WHEN cond THEN ("t"."id") END))
//	sum_price(if: cond)        → sum((CASE WHEN cond THEN ("t"."price") END))
//	count_distinct_x(if: cond) → count(DISTINCT (CASE WHEN cond THEN ("t"."x") END))
func (c *compilerContext) renderAggFunctionWithFilter(sel *qcode.Select, f qcode.Field) {
	name, distinct := strings.CutSuffix(f.Func.Name, "_distinct")
	if !distinct {
		name = f.Func.Name
	}

	c.w.WriteString(name)
	c.w.WriteString(`(`)
	if distinct {
		c.w.WriteString(`DISTINCT `)
	}
	c.w.WriteString(`(CASE WHEN `)
	c.renderExp(sel.Ti, f.AggFilter.Exp, false)
	c.w.WriteString(` THEN (`)

	i := 0
	for _, a := range f.Args {
		if a.Name == "" {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.renderFuncArgVal(a)
			i++
		}
	}

	c.w.WriteString(`) END)`)
	c.w.WriteString(`)`)
}

func (c *compilerContext) renderFunction(name string, args []qcode.Arg) {
	// Map "<base>_distinct" → "<base>(DISTINCT ...)" so e.g. count_distinct
	// renders as the standard SQL count(DISTINCT col).
	base, distinct := strings.CutSuffix(name, "_distinct")
	if distinct {
		name = base
	}

	c.w.WriteString(name)
	c.w.WriteString(`(`)
	if distinct {
		c.w.WriteString(`DISTINCT `)
	}

	i := 0
	for _, a := range args {
		if a.Name == "" {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.renderFuncArgVal(a)
			i++
		}
	}
	for _, a := range args {
		if a.Name != "" {
			if i != 0 {
				c.w.WriteString(`, `)
			}
			c.w.WriteString(a.Name + ` => `)
			c.renderFuncArgVal(a)
			i++
		}
	}
	_, _ = c.w.WriteString(`)`)
}

func (c *compilerContext) renderFuncArgVal(a qcode.Arg) {
	switch a.Type {
	case qcode.ArgTypeCol:
		c.colWithTable(a.Col.Table, a.Col.Name)
	case qcode.ArgTypeVar:
		c.renderParam(Param{Name: a.Val, Type: a.DType})
		// Add proper casting for JSON/JSONB parameters
		if a.DType == "json" || a.DType == "jsonb" {
			c.w.WriteString(" :: ")
			c.w.WriteString(a.DType)
		}
	default:
		c.squoted(a.Val)
	}
}
