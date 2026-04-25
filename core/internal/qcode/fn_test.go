package qcode_test

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// TestAggFuncDispatchPrefersLongestPrefix ensures count_distinct_<col> binds
// to the count_distinct aggregate, not count with col="distinct_<col>".
//
// Map iteration in Go is randomized, so we run the compile many times to
// defeat a "right by coincidence" match in a buggy implementation.
func TestAggFuncDispatchPrefersLongestPrefix(t *testing.T) {
	qc, err := qcode.NewCompiler(dbs, qcode.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := qc.AddRole("user", "public", "products", qcode.TRConfig{
		Query: qcode.QueryConfig{Columns: []string{"id", "name"}},
	}); err != nil {
		t.Fatal(err)
	}

	gql := []byte(`query { products { count_distinct_id } }`)

	for i := 0; i < 50; i++ {
		out, err := qc.Compile(gql, nil, "user", "")
		if err != nil {
			t.Fatalf("iter %d: compile: %v", i, err)
		}
		if len(out.Selects) == 0 {
			t.Fatalf("iter %d: no selects", i)
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
			t.Fatalf("iter %d: no func field in selects[0]", i)
		}
		if fn.Func.Name != "count_distinct" {
			t.Fatalf("iter %d: matched func = %q, want count_distinct", i, fn.Func.Name)
		}
		if len(fn.Args) != 1 || fn.Args[0].Col.Name != "id" {
			t.Fatalf("iter %d: args = %+v, want single col=id", i, fn.Args)
		}
		if !strings.HasSuffix(fn.FieldName, "count_distinct_id") {
			t.Fatalf("iter %d: FieldName = %q, want suffix count_distinct_id", i, fn.FieldName)
		}
	}
}
