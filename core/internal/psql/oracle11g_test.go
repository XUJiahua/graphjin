package psql_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
)

func TestOracle11gCompileFullQuery(t *testing.T) {
	gql := `query {
		users {
			id
			email
			products {
				name
				price
			}
		}
	}`

	qc, err := qcompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	pc := psql.NewCompiler(psql.Config{DBType: "oracle11g"})
	_, stmt, err := pc.CompileEx(qc)
	if err != nil {
		t.Fatal(err)
	}

	var inst struct {
		Operation string `json:"operation"`
		Queries   []struct {
			ID       int32   `json:"id"`
			Field    string  `json:"field_name"`
			SQL      string  `json:"sql"`
			Children []int32 `json:"children"`
			Params   []struct {
				Type   string `json:"type"`
				Column string `json:"column"`
			} `json:"params"`
		} `json:"queries"`
	}

	if err := json.Unmarshal(stmt, &inst); err != nil {
		t.Fatalf("invalid oracle11g instruction JSON: %v\n%s", err, string(stmt))
	}

	if inst.Operation != "oracle11g_query" {
		t.Fatalf("operation = %q, want oracle11g_query", inst.Operation)
	}

	if len(inst.Queries) != 2 {
		t.Fatalf("len(queries) = %d, want 2", len(inst.Queries))
	}

	root := inst.Queries[0]
	child := inst.Queries[1]

	if root.Field != "users" {
		t.Fatalf("root field = %q, want users", root.Field)
	}
	if child.Field != "products" {
		t.Fatalf("child field = %q, want products", child.Field)
	}

	if len(root.Children) != 1 || root.Children[0] != child.ID {
		t.Fatalf("root children = %v, want [%d]", root.Children, child.ID)
	}

	if !strings.Contains(root.SQL, `"PUBLIC"."USERS"`) {
		t.Fatalf("root SQL missing users table: %s", root.SQL)
	}
	if !strings.Contains(root.SQL, `ROWNUM <= 20`) {
		t.Fatalf("root SQL missing 11g limit wrapper: %s", root.SQL)
	}

	if !strings.Contains(child.SQL, `"PUBLIC"."PRODUCTS"`) {
		t.Fatalf("child SQL missing products table: %s", child.SQL)
	}
	if len(child.Params) == 0 || child.Params[0].Type != "parent" || child.Params[0].Column != "id" {
		t.Fatalf("child params = %+v, want first param to be parent id", child.Params)
	}
}

func TestOracle11gCompileMutationFailsFast(t *testing.T) {
	gql := `mutation {
		products(delete: true, where: { id: { eq: 1 } }) {
			id
		}
	}`

	qc, err := qcompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	pc := psql.NewCompiler(psql.Config{DBType: "oracle11g"})
	_, _, err = pc.CompileEx(qc)
	if err == nil {
		t.Fatal("expected oracle11g mutation compile to fail")
	}
	if got := err.Error(); !strings.Contains(got, "oracle11g does not support mutations") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOracle11gCompileCursorInstruction(t *testing.T) {
	gql := `query {
		products(first: 5, after: $products_cursor, order_by: { price: asc }) {
			name
		}
	}`

	qc, err := qcompile.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	pc := psql.NewCompiler(psql.Config{
		DBType:    "oracle11g",
		SecPrefix: []byte("gj-test:"),
	})
	_, stmt, err := pc.CompileEx(qc)
	if err != nil {
		t.Fatal(err)
	}

	var inst struct {
		Queries []struct {
			ID     int32 `json:"id"`
			Params []struct {
				Type      string `json:"type"`
				Name      string `json:"name"`
				ArgIndex  int    `json:"arg_index"`
				CursorIdx int    `json:"cursor_idx"`
			} `json:"params"`
			Columns []struct {
				Source string `json:"source"`
				Hidden bool   `json:"hidden"`
			} `json:"columns"`
			Cursor *struct {
				ParamName string `json:"param_name"`
				Prefix    string `json:"prefix"`
				SelID     int32  `json:"sel_id"`
				OrderBy   []struct {
					Source string `json:"source"`
				} `json:"order_by"`
			} `json:"cursor"`
		} `json:"queries"`
	}

	if err := json.Unmarshal(stmt, &inst); err != nil {
		t.Fatalf("invalid oracle11g instruction JSON: %v\n%s", err, string(stmt))
	}
	if len(inst.Queries) != 1 {
		t.Fatalf("len(queries) = %d, want 1", len(inst.Queries))
	}

	root := inst.Queries[0]
	if root.Cursor == nil {
		t.Fatal("expected cursor metadata in oracle11g instruction")
	}
	if root.Cursor.ParamName != "products_cursor" {
		t.Fatalf("cursor.param_name = %q, want products_cursor", root.Cursor.ParamName)
	}
	if root.Cursor.Prefix != "gj-test:" {
		t.Fatalf("cursor.prefix = %q, want gj-test:", root.Cursor.Prefix)
	}
	if root.Cursor.SelID != root.ID {
		t.Fatalf("cursor.sel_id = %d, want %d", root.Cursor.SelID, root.ID)
	}
	if len(root.Cursor.OrderBy) != 2 || root.Cursor.OrderBy[0].Source != "price" || root.Cursor.OrderBy[1].Source != "id" {
		t.Fatalf("cursor.order_by = %+v, want [price id]", root.Cursor.OrderBy)
	}

	var hiddenCols []string
	for _, col := range root.Columns {
		if col.Hidden {
			hiddenCols = append(hiddenCols, col.Source)
		}
	}
	if len(hiddenCols) != 2 || hiddenCols[0] != "price" || hiddenCols[1] != "id" {
		t.Fatalf("hidden columns = %v, want [price id]", hiddenCols)
	}

	var cursorParams []struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		ArgIndex  int    `json:"arg_index"`
		CursorIdx int    `json:"cursor_idx"`
	}
	for _, param := range root.Params {
		if param.Type == "cursor" {
			cursorParams = append(cursorParams, param)
		}
	}
	if len(cursorParams) == 0 {
		t.Fatal("expected cursor params in oracle11g instruction")
	}
	for _, param := range cursorParams {
		if param.Name != "products_cursor" {
			t.Fatalf("cursor param name = %q, want products_cursor", param.Name)
		}
		if param.ArgIndex != cursorParams[0].ArgIndex {
			t.Fatalf("cursor arg index mismatch: %+v", cursorParams)
		}
	}
}
