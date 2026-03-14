package psql_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/dialect"
	"github.com/dosco/graphjin/core/v3/internal/psql"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestOracleCompilePreservesOrigNameForNumericColumns(t *testing.T) {
	qc, pc := newOracleRegressionFixture(t, `query {
		inventory_reports {
			overdue_over_180_d_qty
		}
	}`)

	_, sqlBytes, err := pc.CompileEx(qc)
	if err != nil {
		t.Fatal(err)
	}

	sql := string(sqlBytes)
	if !strings.Contains(sql, `"INVENTORY_REPORTS"."OVERDUE_OVER_180D_QTY"`) {
		t.Fatalf("expected SQL to preserve Oracle column identifier on base table reference, got: %s", sql)
	}
	if strings.Contains(sql, `"INVENTORY_REPORTS"."OVERDUE_OVER_180_D_QTY"`) {
		t.Fatalf("expected SQL to avoid reconstructed identifier on base table reference, got: %s", sql)
	}
}

func TestOracleNestedQueryAliasesOrigNumericColumns(t *testing.T) {
	qc, pc := newOracleNestedRegressionFixture(t, `query {
		purchase_order(where: { so_number: { eq: $so_number } }) {
			part_number
			vmi_inventory {
				aging_over_180_d
			}
		}
	}`)

	_, sqlBytes, err := pc.CompileEx(qc)
	if err != nil {
		t.Fatal(err)
	}

	sql := string(sqlBytes)
	if !strings.Contains(sql, `"VMI_INVENTORY"."AGING_OVER_180D" "AGING_OVER_180_D"`) {
		t.Fatalf("expected Oracle base select to alias original numeric column name, got: %s", sql)
	}
	if !strings.Contains(sql, `"VMI_INVENTORY_1"."AGING_OVER_180_D"`) {
		t.Fatalf("expected outer select to keep reading the normalized alias, got: %s", sql)
	}
}

func TestOracleSubscriptionPollPreservesDBIdentifiersButUsesParamNames(t *testing.T) {
	qc, pc := newOracleRegressionFixture(t, `subscription {
		inventory_reports(where: { overdue_over_180_d_qty: { eq: $overdue_over_180_d_qty } }) {
			overdue_over_180_d_qty
		}
	}`)

	md, sqlBytes, err := pc.CompileEx(qc)
	if err != nil {
		t.Fatal(err)
	}

	sql := string(sqlBytes)
	if !strings.Contains(sql, `"INVENTORY_REPORTS"."OVERDUE_OVER_180D_QTY"`) {
		t.Fatalf("expected SQL to preserve original Oracle column identifier, got: %s", sql)
	}
	if !strings.Contains(sql, `"_GJ_SUB"."OVERDUE_OVER_180_D_QTY"`) {
		t.Fatalf("expected poll-mode param references to use GraphQL param identifier, got: %s", sql)
	}

	oracleDialect, ok := pc.GetDialect().(*dialect.OracleDialect)
	if !ok {
		t.Fatalf("expected Oracle dialect, got %T", pc.GetDialect())
	}

	var params []dialect.Param
	for _, p := range md.Params() {
		params = append(params, dialect.Param{
			Name:        p.Name,
			Type:        p.Type,
			IsArray:     p.IsArray,
			IsNotNull:   p.IsNotNull,
			WrapInArray: p.WrapInArray,
		})
	}

	ctx := subscriptionUnboxTestContext{dialect: oracleDialect}
	oracleDialect.RenderSubscriptionUnbox(&ctx, params, sql)

	unboxed := ctx.String()
	if !strings.Contains(unboxed, `WITH "_GJ_SUB" AS (SELECT * FROM JSON_TABLE(`) {
		t.Fatalf("expected Oracle subscription batching wrapper, got: %s", unboxed)
	}
	if !strings.Contains(unboxed, `"OVERDUE_OVER_180_D_QTY" NUMBER PATH '$[0]'`) {
		t.Fatalf("expected subscription params to use GraphQL param identifier, got: %s", unboxed)
	}
}

func newOracleRegressionFixture(t *testing.T, gql string) (*qcode.QCode, *psql.Compiler) {
	t.Helper()

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
			ID:          1,
			Schema:      "public",
			Table:       "inventory_reports",
			Name:        "overdue_over_180_d_qty",
			OrigName:    "OVERDUE_OVER_180D_QTY",
			OrigTable:   "INVENTORY_REPORTS",
			OrigSchema:  "PUBLIC",
			Type:        "integer",
			NotNull:     true,
			FKeySchema:  "",
			FKeyTable:   "",
			FKeyCol:     "",
			OrigFKeyCol: "",
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

	qc, err := qcCompiler.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	pc := psql.NewCompiler(psql.Config{DBType: "oracle"})
	pc.SetSchemaInfo(schema.GetTables())

	return qc, pc
}

func newOracleNestedRegressionFixture(t *testing.T, gql string) (*qcode.QCode, *psql.Compiler) {
	t.Helper()

	cols := []sdata.DBColumn{
		{
			ID:         0,
			Schema:     "public",
			Table:      "purchase_order",
			Name:       "so_number",
			OrigName:   "SO_NUMBER",
			OrigTable:  "PURCHASE_ORDER",
			OrigSchema: "PUBLIC",
			Type:       "varchar2",
			NotNull:    true,
		},
		{
			ID:         1,
			Schema:     "public",
			Table:      "purchase_order",
			Name:       "part_number",
			OrigName:   "PART_NUMBER",
			OrigTable:  "PURCHASE_ORDER",
			OrigSchema: "PUBLIC",
			Type:       "varchar2",
			NotNull:    true,
			UniqueKey:  true,
		},
		{
			ID:             2,
			Schema:         "public",
			Table:          "vmi_inventory",
			Name:           "part_number",
			OrigName:       "PART_NUMBER",
			OrigTable:      "VMI_INVENTORY",
			OrigSchema:     "PUBLIC",
			Type:           "varchar2",
			NotNull:        true,
			FKeySchema:     "public",
			FKeyTable:      "purchase_order",
			FKeyCol:        "part_number",
			OrigFKeySchema: "PUBLIC",
			OrigFKeyTable:  "PURCHASE_ORDER",
			OrigFKeyCol:    "PART_NUMBER",
		},
		{
			ID:         3,
			Schema:     "public",
			Table:      "vmi_inventory",
			Name:       "aging_over_180_d",
			OrigName:   "AGING_OVER_180D",
			OrigTable:  "VMI_INVENTORY",
			OrigSchema: "PUBLIC",
			Type:       "number",
			NotNull:    true,
		},
	}

	dbinfo := sdata.NewDBInfo("oracle", 18, "public", "", cols, nil, nil)
	schema, err := sdata.NewDBSchema(dbinfo, nil)
	if err != nil {
		t.Fatal(err)
	}

	qcCompiler, err := qcode.NewCompiler(schema, qcode.Config{DBSchema: schema.DBSchema()})
	if err != nil {
		t.Fatal(err)
	}

	err = qcCompiler.AddRole("user", "public", "purchase_order", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"so_number", "part_number"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = qcCompiler.AddRole("user", "public", "vmi_inventory", qcode.TRConfig{
		Query: qcode.QueryConfig{
			Columns: []string{"part_number", "aging_over_180_d"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	qc, err := qcCompiler.Compile([]byte(gql), nil, "user", "")
	if err != nil {
		t.Fatal(err)
	}

	pc := psql.NewCompiler(psql.Config{DBType: "oracle", DBVersion: 18})
	pc.SetSchemaInfo(schema.GetTables())

	return qc, pc
}

type subscriptionUnboxTestContext struct {
	buf     bytes.Buffer
	dialect *dialect.OracleDialect
}

func (c *subscriptionUnboxTestContext) Write(s string) (int, error) {
	return c.buf.WriteString(s)
}

func (c *subscriptionUnboxTestContext) WriteString(s string) (int, error) {
	return c.buf.WriteString(s)
}

func (c *subscriptionUnboxTestContext) AddParam(p dialect.Param) string {
	return ""
}

func (c *subscriptionUnboxTestContext) Quote(s string) {
	c.buf.WriteString(c.dialect.QuoteIdentifier(s))
}

func (c *subscriptionUnboxTestContext) ColWithTable(table, col string) {
	c.Quote(table)
	c.buf.WriteString(".")
	c.Quote(col)
}

func (c *subscriptionUnboxTestContext) RenderJSONFields(sel *qcode.Select) {}

func (c *subscriptionUnboxTestContext) IsTableMutated(table string) bool {
	return false
}

func (c *subscriptionUnboxTestContext) RenderExp(ti sdata.DBTable, ex *qcode.Exp) {}

func (c *subscriptionUnboxTestContext) GetStaticVar(name string) (string, bool) {
	return "", false
}

func (c *subscriptionUnboxTestContext) GetSecPrefix() string {
	return ""
}

func (c *subscriptionUnboxTestContext) String() string {
	return c.buf.String()
}
