package dialect

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestOracle11gWrapPagingOffsetOnlyDoesNotClipRows(t *testing.T) {
	b := oracle11gSQLBuilder{
		sel: &qcode.Select{
			Paging: qcode.Paging{Offset: 10},
			BCols:  []qcode.Column{{Col: sdata.DBColumn{Name: "id"}}},
		},
	}

	got := b.wrapPaging(`SELECT "ID" FROM "USERS"`)

	if strings.Contains(got, `ROWNUM <= 10`) {
		t.Fatalf("offset-only paging should not add inner ROWNUM cap: %s", got)
	}
	if !strings.Contains(got, `"__GJ_RN" > 10`) {
		t.Fatalf("offset-only paging missing outer rownum filter: %s", got)
	}
}

func TestOracle11gWriteLiteralRejectsInvalidNumericLiteral(t *testing.T) {
	state := &oracle11gCompileState{}
	b := oracle11gSQLBuilder{state: state}

	var buf bytes.Buffer
	b.writeLiteral(&buf, `1 OR 1=1`, qcode.ValNum)

	if state.err == nil {
		t.Fatal("expected invalid numeric literal to set an error")
	}
	if got := buf.String(); got != "NULL" {
		t.Fatalf("writeLiteral() = %q, want NULL", got)
	}
}

func TestOracle11gWrapPagingUsesNormalizedAliasesForOrigColumns(t *testing.T) {
	d := &Oracle11gDialect{}
	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Paging: qcode.Paging{Offset: 5},
			Ti:     sdata.DBTable{Name: "inventory_reports", Schema: "public"},
			BCols: []qcode.Column{{
				Col: sdata.DBColumn{
					Name:     "overdue_over_180_d_qty",
					OrigName: "OVERDUE_OVER_180D_QTY",
				},
			}},
		},
	}

	b.initProjections()
	got := b.build()

	if !strings.Contains(got, `"OVERDUE_OVER_180D_QTY" AS "OVERDUE_OVER_180_D_QTY"`) {
		t.Fatalf("expected inner query to alias original Oracle column name, got: %s", got)
	}
	if !strings.Contains(got, `GJ_RN__."OVERDUE_OVER_180_D_QTY"`) {
		t.Fatalf("expected outer offset wrapper to read normalized alias, got: %s", got)
	}
}

func TestOracle11gWriteTableUsesOriginalSchemaAndTableNames(t *testing.T) {
	d := &Oracle11gDialect{}
	d.SetNameMap([]sdata.DBTable{{
		Name:       "purchase_order",
		OrigName:   "PurchaseOrder",
		Schema:     "sales_ops",
		OrigSchema: "SalesOps",
	}})

	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Ti: sdata.DBTable{
				Name:   "purchase_order",
				Schema: "sales_ops",
			},
		},
	}

	var buf bytes.Buffer
	b.writeTable(&buf)

	if got := buf.String(); got != `"SalesOps"."PurchaseOrder"` {
		t.Fatalf("writeTable() = %q, want %q", got, `"SalesOps"."PurchaseOrder"`)
	}
}

func TestOracle11gCompileSelectSkipsRemoteAndDatabaseJoinChildren(t *testing.T) {
	state := &oracle11gCompileState{
		qc: &qcode.QCode{
			Selects: []qcode.Select{
				{
					Field: qcode.Field{ID: 0, FieldName: "users"},
					Table: "users",
					Ti: sdata.DBTable{
						Name:   "users",
						Schema: "public",
					},
					BCols: []qcode.Column{
						{Col: sdata.DBColumn{Name: "id"}},
					},
					Children: []int32{1, 2, 3},
				},
				{
					Field: qcode.Field{ID: 1, FieldName: "products"},
				},
				{
					Field: qcode.Field{ID: 2, FieldName: "profile", SkipRender: qcode.SkipTypeRemote},
				},
				{
					Field: qcode.Field{ID: 3, FieldName: "orders", SkipRender: qcode.SkipTypeDatabaseJoin},
				},
			},
		},
		dialect: &Oracle11gDialect{},
	}

	root := &state.qc.Selects[0]
	root.Children = []int32{1, 2, 3}
	state.qc.Selects[1].Field.SkipRender = qcode.SkipTypeNone

	q := state.compileSelect(root)

	if len(q.Children) != 1 || q.Children[0] != 1 {
		t.Fatalf("children = %v, want only local child [1]", q.Children)
	}
}

func TestOracle11gCompileTreeSkipsDroppedRoot(t *testing.T) {
	state := &oracle11gCompileState{
		qc: &qcode.QCode{
			Selects: []qcode.Select{{
				Field: qcode.Field{
					ID:         0,
					FieldName:  "users",
					SkipRender: qcode.SkipTypeDrop,
				},
				Table: "users",
				Ti: sdata.DBTable{
					Name:   "users",
					Schema: "public",
				},
			}},
		},
		dialect: &Oracle11gDialect{},
	}

	var out []oracle11gQuery
	state.compileTree(&state.qc.Selects[0], &out)

	if len(out) != 0 {
		t.Fatalf("compileTree() emitted %d plans for dropped root, want 0", len(out))
	}
}

func TestOracle11gWriteColumnsRendersAggregateFunction(t *testing.T) {
	d := &Oracle11gDialect{}
	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Field: qcode.Field{ID: 0, FieldName: "summary"},
			Ti:    sdata.DBTable{Name: "orders", Schema: "public"},
			Fields: []qcode.Field{
				{
					Type:      qcode.FieldTypeFunc,
					FieldName: "count_id",
					Func:      sdata.DBFunction{Name: "count", Type: "bigint"},
					Args: []qcode.Arg{
						{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "id"}},
					},
				},
			},
			GroupCols: true,
		},
	}

	b.initProjections()
	got := b.build()

	if !strings.Contains(got, `COUNT("ID") AS "COUNT_ID"`) {
		t.Fatalf("expected COUNT(\"ID\") AS \"COUNT_ID\" in SQL, got: %s", got)
	}
	if !strings.Contains(got, `FROM "PUBLIC"."ORDERS"`) {
		t.Fatalf("expected FROM \"PUBLIC\".\"ORDERS\", got: %s", got)
	}
}

func TestOracle11gWriteColumnsRendersMultipleAggregates(t *testing.T) {
	d := &Oracle11gDialect{}
	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Field: qcode.Field{ID: 0, FieldName: "summary"},
			Ti:    sdata.DBTable{Name: "orders", Schema: "public"},
			Fields: []qcode.Field{
				{
					Type:      qcode.FieldTypeFunc,
					FieldName: "count_id",
					Func:      sdata.DBFunction{Name: "count", Type: "bigint"},
					Args:      []qcode.Arg{{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "id"}}},
				},
				{
					Type:      qcode.FieldTypeFunc,
					FieldName: "sum_amount",
					Func:      sdata.DBFunction{Name: "sum", Type: "bigint"},
					Args:      []qcode.Arg{{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "amount"}}},
				},
			},
			GroupCols: true,
		},
	}

	b.initProjections()
	got := b.build()

	if !strings.Contains(got, `COUNT("ID") AS "COUNT_ID", SUM("AMOUNT") AS "SUM_AMOUNT"`) {
		t.Fatalf("expected both aggregates comma-separated, got: %s", got)
	}
}

func TestOracle11gAggregateWithSingularStillCapsRownum(t *testing.T) {
	d := &Oracle11gDialect{}
	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Field:    qcode.Field{ID: 0, FieldName: "summary"},
			Ti:       sdata.DBTable{Name: "orders", Schema: "public"},
			Singular: true,
			Fields: []qcode.Field{
				{
					Type:      qcode.FieldTypeFunc,
					FieldName: "count_id",
					Func:      sdata.DBFunction{Name: "count", Type: "bigint"},
					Args:      []qcode.Arg{{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "id"}}},
				},
			},
			GroupCols: true,
		},
	}

	b.initProjections()
	got := b.build()

	if !strings.Contains(got, `ROWNUM <= 1`) {
		t.Fatalf("expected ROWNUM <= 1 for @object aggregate, got: %s", got)
	}
	if !strings.Contains(got, `COUNT("ID") AS "COUNT_ID"`) {
		t.Fatalf("expected COUNT(\"ID\") AS \"COUNT_ID\", got: %s", got)
	}
}

func TestOracle11gAggregateUsesOriginalColumnName(t *testing.T) {
	d := &Oracle11gDialect{}
	d.SetNameMap([]sdata.DBTable{{
		Name:     "orders",
		OrigName: "Orders",
		Columns: []sdata.DBColumn{{
			Name:     "row_id",
			OrigName: "ROW_ID",
			Table:    "orders",
		}},
	}})

	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Field: qcode.Field{ID: 0, FieldName: "summary"},
			Ti:    sdata.DBTable{Name: "orders", Schema: "public"},
			Fields: []qcode.Field{
				{
					Type:      qcode.FieldTypeFunc,
					FieldName: "count_row_id",
					Func:      sdata.DBFunction{Name: "count", Type: "bigint"},
					Args: []qcode.Arg{
						{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "row_id", OrigName: "ROW_ID"}},
					},
				},
			},
			GroupCols: true,
		},
	}

	b.initProjections()
	got := b.build()

	if !strings.Contains(got, `COUNT("ROW_ID")`) {
		t.Fatalf("expected aggregate to reference original column name, got: %s", got)
	}
}

func TestOracle11gCompileSelectEmitsAggregateColumnsInPlan(t *testing.T) {
	state := &oracle11gCompileState{
		qc: &qcode.QCode{
			Selects: []qcode.Select{{
				Field: qcode.Field{ID: 0, FieldName: "summary"},
				Table: "orders",
				Ti:    sdata.DBTable{Name: "orders", Schema: "public"},
				Fields: []qcode.Field{
					{
						Type:      qcode.FieldTypeFunc,
						FieldName: "count_id",
						Func:      sdata.DBFunction{Name: "count", Type: "bigint"},
						Args:      []qcode.Arg{{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "id"}}},
					},
				},
				GroupCols: true,
			}},
		},
		dialect: &Oracle11gDialect{},
	}

	q := state.compileSelect(&state.qc.Selects[0])

	if len(q.Columns) != 1 {
		t.Fatalf("plan.Columns length = %d, want 1", len(q.Columns))
	}
	if q.Columns[0].FieldName != "count_id" {
		t.Fatalf("plan.Columns[0].FieldName = %q, want %q", q.Columns[0].FieldName, "count_id")
	}
	if q.Columns[0].Source != "count_id" {
		t.Fatalf("plan.Columns[0].Source = %q, want %q", q.Columns[0].Source, "count_id")
	}
	if !strings.Contains(q.SQL, `COUNT("ID") AS "COUNT_ID"`) {
		t.Fatalf("plan.SQL missing aggregate: %s", q.SQL)
	}
}

func TestOracle11gWriteColumnsRendersCountDistinct(t *testing.T) {
	d := &Oracle11gDialect{}
	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Field: qcode.Field{ID: 0, FieldName: "summary"},
			Ti:    sdata.DBTable{Name: "orders", Schema: "public"},
			Fields: []qcode.Field{
				{
					Type:      qcode.FieldTypeFunc,
					FieldName: "count_distinct_customer_id",
					Func:      sdata.DBFunction{Name: "count_distinct", Type: "bigint"},
					Args: []qcode.Arg{
						{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "customer_id"}},
					},
				},
			},
			GroupCols: true,
		},
	}

	b.initProjections()
	got := b.build()

	if !strings.Contains(got, `COUNT(DISTINCT "CUSTOMER_ID") AS "COUNT_DISTINCT_CUSTOMER_ID"`) {
		t.Fatalf("expected COUNT(DISTINCT ...) rendering, got: %s", got)
	}
}

func TestOracle11gWriteColumnsRendersConditionalAggregate(t *testing.T) {
	d := &Oracle11gDialect{}
	gtCol := sdata.DBColumn{Name: "price", Table: "orders"}
	cond := &qcode.Exp{Op: qcode.OpGreaterThan}
	cond.Left.Col = gtCol
	cond.Left.ColName = "price"
	cond.Right.ValType = qcode.ValNum
	cond.Right.Val = "10"

	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Field: qcode.Field{ID: 0, FieldName: "summary"},
			Ti:    sdata.DBTable{Name: "orders", Schema: "public"},
			Fields: []qcode.Field{
				{
					Type:      qcode.FieldTypeFunc,
					FieldName: "expensive_count",
					Func:      sdata.DBFunction{Name: "count", Type: "bigint"},
					Args: []qcode.Arg{
						{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "id"}},
					},
					AggFilter: qcode.Filter{Exp: cond},
				},
			},
			GroupCols: true,
		},
	}

	b.initProjections()
	got := b.build()

	if !strings.Contains(got, `COUNT((CASE WHEN`) {
		t.Fatalf("expected COUNT((CASE WHEN ...), got: %s", got)
	}
	if !strings.Contains(got, `THEN ("ID") END))`) {
		t.Fatalf("expected CASE THEN col END inside COUNT, got: %s", got)
	}
	if !strings.Contains(got, `AS "EXPENSIVE_COUNT"`) {
		t.Fatalf("expected aliased to EXPENSIVE_COUNT, got: %s", got)
	}
}

func TestOracle11gAggregateWithBaseColsEmitsGroupBy(t *testing.T) {
	d := &Oracle11gDialect{}
	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Field: qcode.Field{ID: 0, FieldName: "summary"},
			Ti:    sdata.DBTable{Name: "orders", Schema: "public"},
			BCols: []qcode.Column{{
				Col:       sdata.DBColumn{Name: "category"},
				FieldName: "category",
			}},
			Fields: []qcode.Field{
				{
					Type:      qcode.FieldTypeFunc,
					FieldName: "count_id",
					Func:      sdata.DBFunction{Name: "count", Type: "bigint"},
					Args:      []qcode.Arg{{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "id"}}},
				},
			},
			GroupCols: true,
		},
	}

	b.initProjections()
	got := b.build()

	if !strings.Contains(got, `GROUP BY "CATEGORY"`) {
		t.Fatalf("expected GROUP BY \"CATEGORY\", got: %s", got)
	}
}

func TestOracle11gPureAggregateOmitsGroupBy(t *testing.T) {
	d := &Oracle11gDialect{}
	b := oracle11gSQLBuilder{
		state: &oracle11gCompileState{dialect: d},
		sel: &qcode.Select{
			Field: qcode.Field{ID: 0, FieldName: "summary"},
			Ti:    sdata.DBTable{Name: "orders", Schema: "public"},
			Fields: []qcode.Field{
				{
					Type:      qcode.FieldTypeFunc,
					FieldName: "count_id",
					Func:      sdata.DBFunction{Name: "count", Type: "bigint"},
					Args:      []qcode.Arg{{Type: qcode.ArgTypeCol, Col: sdata.DBColumn{Name: "id"}}},
				},
			},
			GroupCols: true,
		},
	}

	b.initProjections()
	got := b.build()

	if strings.Contains(got, `GROUP BY`) {
		t.Fatalf("pure-aggregate query should not emit GROUP BY, got: %s", got)
	}
}

func TestOracle11gWriteOrderByPreservesNullOrdering(t *testing.T) {
	b := oracle11gSQLBuilder{
		sel: &qcode.Select{
			OrderBy: []qcode.OrderBy{
				{Col: sdata.DBColumn{Name: "price"}, Order: qcode.OrderAscNullsFirst},
				{Col: sdata.DBColumn{Name: "created_at"}, Order: qcode.OrderDescNullsLast},
			},
		},
	}

	var buf bytes.Buffer
	b.writeOrderBy(&buf)

	got := buf.String()
	if !strings.Contains(got, `"PRICE" ASC NULLS FIRST`) {
		t.Fatalf("order by missing ASC NULLS FIRST: %s", got)
	}
	if !strings.Contains(got, `"CREATED_AT" DESC NULLS LAST`) {
		t.Fatalf("order by missing DESC NULLS LAST: %s", got)
	}
}
