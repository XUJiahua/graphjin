package core

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestRenderSubWrapUsesOracleParamIdentifiers(t *testing.T) {
	cols := []sdata.DBColumn{
		{
			ID:         0,
			Schema:     "public",
			Table:      "inventory_reports",
			Name:       "id",
			OrigName:   "ID",
			OrigTable:  "INVENTORY_REPORTS",
			OrigSchema: "PUBLIC",
			Type:       "integer",
			PrimaryKey: true,
			NotNull:    true,
		},
		{
			ID:         1,
			Schema:     "public",
			Table:      "inventory_reports",
			Name:       "overdue_over_180_d_qty",
			OrigName:   "OVERDUE_OVER_180D_QTY",
			OrigTable:  "INVENTORY_REPORTS",
			OrigSchema: "PUBLIC",
			Type:       "integer",
			NotNull:    true,
		},
	}

	dbinfo := sdata.NewDBInfo("oracle", 12, "public", "", cols, nil, nil)
	schema, err := sdata.NewDBSchema(dbinfo, nil)
	if err != nil {
		t.Fatal(err)
	}

	qcCompiler, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}

	err = qcCompiler.AddRole("user", "public", "inventory_reports", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"id", "overdue_over_180_d_qty"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcCompiler.Compile([]byte(`subscription {
		inventory_reports(where: { overdue_over_180_d_qty: { eq: $overdue_over_180_d_qty } }) {
			overdue_over_180_d_qty
		}
	}`), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	pc := psql.NewCompiler(psql.Config{DBType: "oracle"})
	pc.SetSchemaInfo(schema.GetTables())

	md, sqlBytes, err := pc.CompileEx(qc)
	if err != nil {
		t.Fatal(err)
	}

	wrapped := renderSubWrap(stmt{md: md, sql: string(sqlBytes)}, pc.GetDialect())
	if !strings.Contains(wrapped, `"OVERDUE_OVER_180_D_QTY" NUMBER PATH '$[0]'`) {
		t.Fatalf("expected subscription wrapper to use GraphQL param identifier, got: %s", wrapped)
	}
	if strings.Contains(wrapped, `"OVERDUE_OVER_180D_QTY" NUMBER PATH '$[0]'`) {
		t.Fatalf("unexpected database column identifier in wrapper param schema: %s", wrapped)
	}
}
