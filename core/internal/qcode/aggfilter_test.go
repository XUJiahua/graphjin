package qcode_test

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// TestAggFuncIfArgPopulatesAggFilter parses the `if:` argument on an aggregate
// field into Field.AggFilter so dialects can emit
// FUNC(CASE WHEN <cond> THEN <col> END).
func TestAggFuncIfArgPopulatesAggFilter(t *testing.T) {
	qc, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name", "price"}},
	}); err != nil {
		t.Fatal(err)
	}

	gql := []byte(`query { products { active_count: count_id(if: { price: { gt: 10 } }) } }`)

	out, err := qc.Compile(gql, nil, "user", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Selects) == 0 {
		t.Fatal("no selects")
	}
	sel := out.Selects[0]

	var fn *qcode.Field
	for j := range sel.Fields {
		if sel.Fields[j].Type == qcode.FieldTypeFunc {
			fn = &sel.Fields[j]
			break
		}
	}
	if fn == nil {
		t.Fatal("no func field")
	}
	if fn.AggFilter.Exp == nil {
		t.Fatal("expected AggFilter.Exp to be populated by if: arg, got nil")
	}
}
