package dialect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// Oracle11gDialect embeds PostgresDialect only to satisfy the broad Dialect
// interface. The read path is fully custom via CompileFullQuery below, so keep
// inherited Postgres methods audited if the oracle11g surface area expands.
type Oracle11gDialect struct {
	PostgresDialect
	EnableCamelcase bool
	SchemaNameMap   map[string]string
	TableNameMap    map[string]string
	ColumnNameMap   map[string]map[string]string
}

// Keep this instruction schema in sync with oracle11gdriver/executor.go.
type oracle11gInstruction struct {
	Operation     string           `json:"operation"`
	Error         string           `json:"error,omitempty"`
	QueryName     string           `json:"query_name,omitempty"`
	QueryTypename string           `json:"query_typename,omitempty"`
	Queries       []oracle11gQuery `json:"queries,omitempty"`
}

type oracle11gQuery struct {
	ID        int32             `json:"id"`
	FieldName string            `json:"field_name"`
	Table     string            `json:"table"`
	Schema    string            `json:"schema,omitempty"`
	SQL       string            `json:"sql,omitempty"`
	Params    []oracle11gParam  `json:"params,omitempty"`
	Columns   []oracle11gColumn `json:"columns,omitempty"`
	Cursor    *oracle11gCursor  `json:"cursor,omitempty"`
	Singular  bool              `json:"singular"`
	Typename  bool              `json:"typename,omitempty"`
	Null      bool              `json:"null,omitempty"`
	Children  []int32           `json:"children,omitempty"`
}

type oracle11gParam struct {
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	ArgIndex  int    `json:"arg_index,omitempty"`
	ParentID  int32  `json:"parent_id,omitempty"`
	Column    string `json:"column,omitempty"`
	CursorIdx int    `json:"cursor_idx,omitempty"`
	ValueType string `json:"value_type,omitempty"`
}

type oracle11gColumn struct {
	Source    string `json:"source"`
	FieldName string `json:"field_name,omitempty"`
	ValueType string `json:"value_type,omitempty"`
	Hidden    bool   `json:"hidden,omitempty"`
}

type oracle11gCursor struct {
	ParamName string                  `json:"param_name,omitempty"`
	Prefix    string                  `json:"prefix,omitempty"`
	SelID     int32                   `json:"sel_id,omitempty"`
	OrderBy   []oracle11gCursorColumn `json:"order_by,omitempty"`
}

type oracle11gCursorColumn struct {
	Source    string `json:"source"`
	ValueType string `json:"value_type,omitempty"`
}

type oracle11gProjection struct {
	Col       sdata.DBColumn
	Source    string
	FieldName string
	ValueType string
	Hidden    bool
	// AggFunc, when non-empty, makes this projection render as an aggregate
	// expression (e.g. COUNT("ID")) aliased to Source. AggArgs supplies the
	// function arguments. AggFunc takes precedence over Col-based rendering.
	AggFunc string
	AggArgs []qcode.Arg
	// AggFilter, when non-nil, wraps each aggregate argument in
	// CASE WHEN <AggFilter> THEN <arg> END so the aggregate only consumes
	// rows matching the condition. Sourced from Field.AggFilter (the `if:`
	// GraphQL argument).
	AggFilter *qcode.Exp
}

type oracle11gCompileState struct {
	ctx            Context
	qc             *qcode.QCode
	dialect        *Oracle11gDialect
	argIndex       int
	cursorArgIndex map[string]int
	err            error
}

type oracle11gSQLBuilder struct {
	state       *oracle11gCompileState
	sel         *qcode.Select
	params      []oracle11gParam
	projections []oracle11gProjection
	bindIndex   int
}

func (d *Oracle11gDialect) Name() string {
	return "oracle11g"
}

func (d *Oracle11gDialect) QuoteIdentifier(s string) string {
	return `"` + strings.ToUpper(s) + `"`
}

func (d *Oracle11gDialect) SetNameMap(tables []sdata.DBTable) {
	d.SchemaNameMap = make(map[string]string)
	d.TableNameMap = make(map[string]string)
	d.ColumnNameMap = make(map[string]map[string]string)

	for _, t := range tables {
		addOracleScopedNameMapEntry(d.SchemaNameMap, t.Schema, t.OrigSchema)
		addOracleScopedNameMapEntry(d.TableNameMap, t.Name, t.OrigName)
		for _, c := range t.Columns {
			addOracleColumnNameMapEntry(d.ColumnNameMap, t.Name, c.Name, c.OrigName)
			addOracleScopedNameMapEntry(d.TableNameMap, c.FKeyTable, c.OrigFKeyTable)
			addOracleScopedNameMapEntry(d.SchemaNameMap, c.FKeySchema, c.OrigFKeySchema)
		}
	}
}

func (d *Oracle11gDialect) QuoteSchemaIdentifier(schema string) string {
	if orig, ok := d.SchemaNameMap[schema]; ok {
		return `"` + orig + `"`
	}
	return d.QuoteIdentifier(schema)
}

func (d *Oracle11gDialect) QuoteTableIdentifier(table string) string {
	if orig, ok := d.TableNameMap[table]; ok {
		return `"` + orig + `"`
	}
	return d.QuoteIdentifier(table)
}

func (d *Oracle11gDialect) QuoteColumnIdentifier(table, column string) string {
	if cols, ok := d.ColumnNameMap[table]; ok {
		if orig, ok := cols[column]; ok {
			return `"` + orig + `"`
		}
	}
	return d.QuoteIdentifier(column)
}

func (d *Oracle11gDialect) BindVar(i int) string {
	return ":" + strconv.Itoa(i)
}

func (d *Oracle11gDialect) UseNamedParams() bool {
	return false
}

func (d *Oracle11gDialect) SupportsLateral() bool {
	return false
}

func (d *Oracle11gDialect) SupportsReturning() bool {
	return false
}

func (d *Oracle11gDialect) SupportsWritableCTE() bool {
	return false
}

func (d *Oracle11gDialect) SupportsSubscriptionBatching() bool {
	return false
}

func (d *Oracle11gDialect) RequiresJSONAsString() bool {
	return true
}

func (d *Oracle11gDialect) RequiresLowercaseIdentifiers() bool {
	return true
}

func (d *Oracle11gDialect) RequiresBooleanAsInt() bool {
	return true
}

func (d *Oracle11gDialect) CompileFullQuery(ctx Context, qc *qcode.QCode) bool {
	inst := oracle11gInstruction{
		Operation: "oracle11g_query",
		QueryName: qc.Name,
	}

	if qc.Typename {
		inst.QueryTypename = qc.Name
	}

	state := oracle11gCompileState{
		ctx:            ctx,
		qc:             qc,
		dialect:        d,
		cursorArgIndex: make(map[string]int),
	}
	inst.Queries = make([]oracle11gQuery, 0, len(qc.Selects))

	for _, rootID := range qc.Roots {
		sel := &qc.Selects[rootID]
		state.compileTree(sel, &inst.Queries)
	}

	if state.err != nil {
		inst.Error = state.err.Error()
		inst.Queries = nil
	}

	b, err := json.Marshal(inst)
	if err != nil {
		fallback := oracle11gInstruction{
			Operation: "oracle11g_query",
			Error:     err.Error(),
		}
		b, _ = json.Marshal(fallback)
	}
	ctx.WriteString(string(b))

	return true
}

func (s *oracle11gCompileState) compileTree(sel *qcode.Select, out *[]oracle11gQuery) {
	if sel.SkipRender == qcode.SkipTypeDrop || sel.Field.SkipRender == qcode.SkipTypeDrop {
		return
	}

	q := s.compileSelect(sel)
	*out = append(*out, q)

	for _, childID := range q.Children {
		child := &s.qc.Selects[childID]
		s.compileTree(child, out)
	}
}

func (s *oracle11gCompileState) compileSelect(sel *qcode.Select) oracle11gQuery {
	q := oracle11gQuery{
		ID:        sel.ID,
		FieldName: sel.FieldName,
		Table:     sel.Table,
		Schema:    sel.Ti.Schema,
		Singular:  sel.Singular,
		Typename:  sel.Typename,
	}

	if sel.Field.SkipRender == qcode.SkipTypeNulled ||
		sel.Field.SkipRender == qcode.SkipTypeUserNeeded ||
		sel.Field.SkipRender == qcode.SkipTypeBlocked ||
		sel.SkipRender == qcode.SkipTypeNulled ||
		sel.SkipRender == qcode.SkipTypeUserNeeded ||
		sel.SkipRender == qcode.SkipTypeBlocked {
		q.Null = true
		return q
	}

	if sel.SkipRender == qcode.SkipTypeRemote ||
		sel.SkipRender == qcode.SkipTypeDatabaseJoin ||
		sel.Field.SkipRender == qcode.SkipTypeRemote ||
		sel.Field.SkipRender == qcode.SkipTypeDatabaseJoin {
		return q
	}

	if len(sel.Joins) != 0 {
		s.setErr(fmt.Errorf("oracle11g does not support multi-hop joins for field %q", sel.FieldName))
		return q
	}

	builder := oracle11gSQLBuilder{
		state: s,
		sel:   sel,
	}
	builder.initProjections()
	q.SQL = builder.build()
	q.Params = builder.params
	q.Columns = make([]oracle11gColumn, 0, len(builder.projections))

	for _, col := range builder.projections {
		q.Columns = append(q.Columns, oracle11gColumn{
			Source:    col.Source,
			FieldName: col.FieldName,
			ValueType: col.ValueType,
			Hidden:    col.Hidden,
		})
	}

	if cursor := builder.cursorInfo(); cursor != nil {
		q.Cursor = cursor
	}

	for _, childID := range sel.Children {
		child := &s.qc.Selects[childID]
		if child.SkipRender == qcode.SkipTypeDrop ||
			child.SkipRender == qcode.SkipTypeRemote ||
			child.SkipRender == qcode.SkipTypeDatabaseJoin ||
			child.Field.SkipRender == qcode.SkipTypeDrop ||
			child.Field.SkipRender == qcode.SkipTypeRemote ||
			child.Field.SkipRender == qcode.SkipTypeDatabaseJoin {
			continue
		}
		q.Children = append(q.Children, childID)
	}

	return q
}

func (s *oracle11gCompileState) setErr(err error) {
	if s.err == nil {
		s.err = err
	}
}

func (b *oracle11gSQLBuilder) build() string {
	var inner bytes.Buffer

	inner.WriteString("SELECT ")
	b.writeColumns(&inner)
	inner.WriteString(" FROM ")
	b.writeTable(&inner)

	if b.sel.Where.Exp != nil {
		inner.WriteString(" WHERE ")
		b.writeExp(&inner, b.sel.Where.Exp)
	}

	b.writeGroupBy(&inner)

	if len(b.sel.OrderBy) != 0 {
		inner.WriteString(" ORDER BY ")
		b.writeOrderBy(&inner)
	}

	return b.wrapPaging(inner.String())
}

func (b *oracle11gSQLBuilder) writeGroupBy(buf *bytes.Buffer) {
	if !b.sel.GroupCols || len(b.sel.BCols) == 0 {
		return
	}
	buf.WriteString(" GROUP BY ")
	for i, col := range b.sel.BCols {
		if i != 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(b.quote(b.dbColName(col.Col)))
	}
}

func (b *oracle11gSQLBuilder) initProjections() {
	if len(b.projections) != 0 {
		return
	}

	seen := make(map[string]struct{}, len(b.sel.BCols)+len(b.sel.OrderBy))
	for _, col := range b.sel.BCols {
		fieldName := col.FieldName
		hidden := fieldName == "" || strings.HasPrefix(fieldName, "__gj_")
		b.projections = append(b.projections, oracle11gProjection{
			Col:       col.Col,
			Source:    col.Col.Name,
			FieldName: fieldName,
			ValueType: col.Col.Type,
			Hidden:    hidden,
		})
		seen[col.Col.Name] = struct{}{}
	}

	for _, f := range b.sel.Fields {
		if f.Type != qcode.FieldTypeFunc {
			continue
		}
		if f.SkipRender == qcode.SkipTypeDrop ||
			f.SkipRender == qcode.SkipTypeRemote ||
			f.SkipRender == qcode.SkipTypeDatabaseJoin {
			continue
		}
		b.projections = append(b.projections, oracle11gProjection{
			Source:    f.FieldName,
			FieldName: f.FieldName,
			ValueType: f.Func.Type,
			AggFunc:   f.Func.Name,
			AggArgs:   f.Args,
			AggFilter: f.AggFilter.Exp,
		})
	}

	if !b.sel.Paging.Cursor {
		return
	}

	for _, ob := range b.sel.OrderBy {
		if _, ok := seen[ob.Col.Name]; ok {
			continue
		}
		b.projections = append(b.projections, oracle11gProjection{
			Col:       ob.Col,
			Source:    ob.Col.Name,
			ValueType: ob.Col.Type,
			Hidden:    true,
		})
		seen[ob.Col.Name] = struct{}{}
	}
}

func (b *oracle11gSQLBuilder) cursorInfo() *oracle11gCursor {
	if !b.sel.Paging.Cursor || len(b.sel.OrderBy) == 0 {
		return nil
	}

	paramName := b.sel.Paging.CursorVar
	if paramName == "" {
		paramName = "cursor"
	}

	info := &oracle11gCursor{
		ParamName: paramName,
		Prefix:    b.state.ctx.GetSecPrefix(),
		SelID:     b.sel.ID,
		OrderBy:   make([]oracle11gCursorColumn, 0, len(b.sel.OrderBy)),
	}
	for _, ob := range b.sel.OrderBy {
		info.OrderBy = append(info.OrderBy, oracle11gCursorColumn{
			Source:    ob.Col.Name,
			ValueType: ob.Col.Type,
		})
	}
	return info
}

func (b *oracle11gSQLBuilder) writeColumns(buf *bytes.Buffer) {
	for i, col := range b.projections {
		if i != 0 {
			buf.WriteString(", ")
		}
		if col.AggFunc != "" {
			b.writeAggregate(buf, col)
			buf.WriteString(" AS ")
			buf.WriteString(b.quote(col.Source))
			continue
		}
		source := b.dbColName(col.Col)
		buf.WriteString(b.quote(source))
		if source != col.Source {
			buf.WriteString(" AS ")
			buf.WriteString(b.quote(col.Source))
		}
	}
}

func (b *oracle11gSQLBuilder) writeAggregate(buf *bytes.Buffer, p oracle11gProjection) {
	name, distinct := normalizeAggName(p.AggFunc)
	buf.WriteString(strings.ToUpper(name))
	buf.WriteString("(")
	if distinct {
		buf.WriteString("DISTINCT ")
	}
	if p.AggFilter != nil {
		buf.WriteString("(CASE WHEN ")
		b.writeExp(buf, p.AggFilter)
		buf.WriteString(" THEN (")
	}
	first := true
	for _, a := range p.AggArgs {
		if a.Name != "" {
			continue
		}
		if !first {
			buf.WriteString(", ")
		}
		first = false
		switch a.Type {
		case qcode.ArgTypeCol:
			buf.WriteString(b.quote(b.dbColName(a.Col)))
		case qcode.ArgTypeVar:
			buf.WriteString(b.addArgParam(a.Val, a.DType))
		default:
			b.writeLiteral(buf, a.Val, qcode.ValStr)
		}
	}
	if p.AggFilter != nil {
		buf.WriteString(") END)")
	}
	buf.WriteString(")")
}

// normalizeAggName strips meta-suffixes that map to SQL modifiers rather than
// distinct functions. Currently only "_distinct" → DISTINCT (e.g. count_distinct
// → COUNT(DISTINCT ...)). Returns the bare aggregate name and the modifier flag.
func normalizeAggName(name string) (base string, distinct bool) {
	if before, ok := strings.CutSuffix(name, "_distinct"); ok {
		return before, true
	}
	return name, false
}

func (b *oracle11gSQLBuilder) writeTable(buf *bytes.Buffer) {
	if schema := b.sel.Ti.Schema; schema != "" {
		buf.WriteString(b.state.dialect.QuoteSchemaIdentifier(schema))
		buf.WriteString(".")
	}
	buf.WriteString(b.state.dialect.QuoteTableIdentifier(b.sel.Ti.Name))
}

func (b *oracle11gSQLBuilder) writeOrderBy(buf *bytes.Buffer) {
	for i, ob := range b.sel.OrderBy {
		if i != 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(b.quote(b.dbColName(ob.Col)))
		switch ob.Order {
		case qcode.OrderDesc:
			buf.WriteString(" DESC")
		case qcode.OrderAscNullsFirst:
			buf.WriteString(" ASC NULLS FIRST")
		case qcode.OrderDescNullsFirst:
			buf.WriteString(" DESC NULLS FIRST")
		case qcode.OrderAscNullsLast:
			buf.WriteString(" ASC NULLS LAST")
		case qcode.OrderDescNullsLast:
			buf.WriteString(" DESC NULLS LAST")
		default:
			buf.WriteString(" ASC")
		}
	}
}

func (b *oracle11gSQLBuilder) wrapPaging(inner string) string {
	limitExpr, hasLimit := b.limitExpr()
	offsetExpr, hasOffset := b.offsetExpr()

	switch {
	case hasOffset:
		var buf bytes.Buffer
		buf.WriteString("SELECT ")
		for i, col := range b.projections {
			if i != 0 {
				buf.WriteString(", ")
			}
			buf.WriteString("GJ_RN__.")
			buf.WriteString(b.quote(col.Source))
		}
		buf.WriteString(" FROM (SELECT GJ_Q__.*, ROWNUM AS ")
		buf.WriteString(b.quote("__gj_rn"))
		buf.WriteString(" FROM (")
		buf.WriteString(inner)
		buf.WriteString(") GJ_Q__")
		if hasLimit {
			buf.WriteString(" WHERE ROWNUM <= ")
			buf.WriteString("(")
			buf.WriteString(offsetExpr)
			buf.WriteString(" + ")
			buf.WriteString(limitExpr)
			buf.WriteString(")")
		}
		buf.WriteString(") GJ_RN__ WHERE ")
		buf.WriteString(b.quote("__gj_rn"))
		buf.WriteString(" > ")
		buf.WriteString(offsetExpr)
		return buf.String()

	case hasLimit:
		return "SELECT * FROM (" + inner + ") GJ_Q__ WHERE ROWNUM <= " + limitExpr

	default:
		return inner
	}
}

func (b *oracle11gSQLBuilder) limitExpr() (string, bool) {
	switch {
	case b.sel.Paging.NoLimit:
		return "", false
	case b.sel.Paging.LimitVar != "":
		return b.addArgParam(b.sel.Paging.LimitVar, "integer"), true
	case b.sel.Paging.Limit > 0:
		return strconv.FormatInt(int64(b.sel.Paging.Limit), 10), true
	case b.sel.Singular:
		return "1", true
	default:
		return "", false
	}
}

func (b *oracle11gSQLBuilder) offsetExpr() (string, bool) {
	switch {
	case b.sel.Paging.OffsetVar != "":
		return b.addArgParam(b.sel.Paging.OffsetVar, "integer"), true
	case b.sel.Paging.Offset > 0:
		return strconv.FormatInt(int64(b.sel.Paging.Offset), 10), true
	default:
		return "", false
	}
}

func (b *oracle11gSQLBuilder) writeExp(buf *bytes.Buffer, ex *qcode.Exp) {
	if ex == nil {
		buf.WriteString("1 = 1")
		return
	}

	switch ex.Op {
	case qcode.OpAnd:
		b.writeLogical(buf, " AND ", ex.Children)
	case qcode.OpOr:
		b.writeLogical(buf, " OR ", ex.Children)
	case qcode.OpNot:
		buf.WriteString("NOT (")
		if len(ex.Children) != 0 {
			b.writeExp(buf, ex.Children[0])
		} else {
			buf.WriteString("1 = 1")
		}
		buf.WriteString(")")
	case qcode.OpFalse:
		buf.WriteString("1 = 0")
	default:
		b.writePredicate(buf, ex)
	}
}

func (b *oracle11gSQLBuilder) writeLogical(buf *bytes.Buffer, join string, children []*qcode.Exp) {
	buf.WriteString("(")
	for i, child := range children {
		if i != 0 {
			buf.WriteString(join)
		}
		b.writeExp(buf, child)
	}
	buf.WriteString(")")
}

func (b *oracle11gSQLBuilder) writePredicate(buf *bytes.Buffer, ex *qcode.Exp) {
	if len(ex.Left.Path) != 0 || len(ex.Right.Path) != 0 || len(ex.Joins) != 0 {
		b.state.setErr(fmt.Errorf("oracle11g does not support nested path filters for field %q", b.sel.FieldName))
		buf.WriteString("1 = 0")
		return
	}

	buf.WriteString("(")

	switch ex.Op {
	case qcode.OpEqualsTrue:
		b.writeColumnRef(buf, ex.Left.Table, ex.Left.Col, ex.Left.ColName)
		buf.WriteString(" = 1)")
		return
	case qcode.OpNotEqualsTrue:
		b.writeColumnRef(buf, ex.Left.Table, ex.Left.Col, ex.Left.ColName)
		buf.WriteString(" <> 1)")
		return
	case qcode.OpIsNull:
		b.writeColumnRef(buf, ex.Left.Table, ex.Left.Col, ex.Left.ColName)
		if strings.EqualFold(ex.Right.Val, "false") {
			buf.WriteString(" IS NOT NULL")
		} else {
			buf.WriteString(" IS NULL")
		}
		buf.WriteString(")")
		return
	case qcode.OpIsNotNull:
		b.writeColumnRef(buf, ex.Left.Table, ex.Left.Col, ex.Left.ColName)
		if strings.EqualFold(ex.Right.Val, "false") {
			buf.WriteString(" IS NULL")
		} else {
			buf.WriteString(" IS NOT NULL")
		}
		buf.WriteString(")")
		return
	case qcode.OpILike:
		buf.WriteString("LOWER(")
		b.writeColumnRef(buf, ex.Left.Table, ex.Left.Col, ex.Left.ColName)
		buf.WriteString(")")
		buf.WriteString(" LIKE ")
		b.writeRightValue(buf, ex, true)
		buf.WriteString(")")
		return
	case qcode.OpNotILike:
		buf.WriteString("LOWER(")
		b.writeColumnRef(buf, ex.Left.Table, ex.Left.Col, ex.Left.ColName)
		buf.WriteString(")")
		buf.WriteString(" NOT LIKE ")
		b.writeRightValue(buf, ex, true)
		buf.WriteString(")")
		return
	}

	b.writeColumnRef(buf, ex.Left.Table, ex.Left.Col, ex.Left.ColName)

	if op, ok := b.opString(ex.Op); ok {
		buf.WriteString(" ")
		buf.WriteString(op)
		buf.WriteString(" ")
		b.writeRightValue(buf, ex, false)
		buf.WriteString(")")
		return
	}

	b.state.setErr(fmt.Errorf("oracle11g does not support operator %v", ex.Op))
	buf.WriteString(" = NULL)")
}

func (b *oracle11gSQLBuilder) writeColumnRef(buf *bytes.Buffer, table string, col sdata.DBColumn, colName string) {
	if table == "__cur" {
		buf.WriteString(b.addCursorParam(colName, col))
		return
	}
	if colName != "" {
		buf.WriteString(b.quote(colName))
		return
	}
	buf.WriteString(b.quote(b.dbColName(col)))
}

func (b *oracle11gSQLBuilder) writeRightValue(buf *bytes.Buffer, ex *qcode.Exp, lower bool) {
	switch {
	case ex.Right.ValType == qcode.ValVar:
		if lower {
			buf.WriteString("LOWER(")
			buf.WriteString(b.addArgParam(ex.Right.Val, ex.Left.Col.Type))
			buf.WriteString(")")
		} else {
			buf.WriteString(b.addArgParam(ex.Right.Val, ex.Left.Col.Type))
		}

	case ex.Right.Col.Name != "":
		if ex.Right.Table == "__cur" {
			if lower {
				buf.WriteString("LOWER(")
				buf.WriteString(b.addCursorParam(ex.Right.ColName, ex.Right.Col))
				buf.WriteString(")")
			} else {
				buf.WriteString(b.addCursorParam(ex.Right.ColName, ex.Right.Col))
			}
			return
		}
		if ex.Right.ID == b.sel.ParentID && b.sel.ParentID != -1 {
			buf.WriteString(b.addParentParam(ex.Right.Col.Name, ex.Right.Col.Type, b.sel.ParentID))
			return
		}
		if lower {
			buf.WriteString("LOWER(")
			buf.WriteString(b.quote(ex.Right.Col.Name))
			buf.WriteString(")")
		} else {
			buf.WriteString(b.quote(b.dbColName(ex.Right.Col)))
		}

	case ex.Right.ValType == qcode.ValList:
		buf.WriteString("(")
		for i, v := range ex.Right.ListVal {
			if i != 0 {
				buf.WriteString(", ")
			}
			b.writeLiteral(buf, v, ex.Right.ListType)
		}
		buf.WriteString(")")

	default:
		if lower {
			buf.WriteString("LOWER(")
			b.writeLiteral(buf, ex.Right.Val, ex.Right.ValType)
			buf.WriteString(")")
		} else {
			b.writeLiteral(buf, ex.Right.Val, ex.Right.ValType)
		}
	}
}

func (b *oracle11gSQLBuilder) writeLiteral(buf *bytes.Buffer, val string, vt qcode.ValType) {
	switch vt {
	case qcode.ValBool:
		if strings.EqualFold(val, "true") {
			buf.WriteString("1")
		} else {
			buf.WriteString("0")
		}
	case qcode.ValNum:
		val = strings.TrimSpace(val)
		num, err := strconv.ParseFloat(val, 64)
		if err != nil || math.IsNaN(num) || math.IsInf(num, 0) {
			b.state.setErr(fmt.Errorf("oracle11g invalid numeric literal %q", val))
			buf.WriteString("NULL")
			return
		}
		buf.WriteString(val)
	case qcode.ValStr:
		buf.WriteString("'")
		buf.WriteString(strings.ReplaceAll(val, "'", "''"))
		buf.WriteString("'")
	case qcode.ValVar:
		buf.WriteString(b.addArgParam(val, "text"))
	default:
		if val == "" {
			buf.WriteString("NULL")
			return
		}
		buf.WriteString("'")
		buf.WriteString(strings.ReplaceAll(val, "'", "''"))
		buf.WriteString("'")
	}
}

func (b *oracle11gSQLBuilder) addArgParam(name, typ string) string {
	b.params = append(b.params, oracle11gParam{
		Type:      "arg",
		Name:      name,
		ArgIndex:  b.state.argIndex,
		ValueType: typ,
	})
	var bind string
	if reg, ok := b.state.ctx.(ParamRegisterer); ok {
		bind = reg.RegisterParam(Param{Name: name, Type: typ})
	} else {
		b.bindIndex++
		bind = ":" + strconv.Itoa(b.bindIndex)
	}
	b.state.argIndex++
	return bind
}

func (b *oracle11gSQLBuilder) addParentParam(col, typ string, parentID int32) string {
	b.bindIndex++
	b.params = append(b.params, oracle11gParam{
		Type:      "parent",
		ParentID:  parentID,
		Column:    col,
		ValueType: typ,
	})
	return ":" + strconv.Itoa(b.bindIndex)
}

func (b *oracle11gSQLBuilder) addCursorParam(colName string, col sdata.DBColumn) string {
	if !b.sel.Paging.Cursor {
		b.state.setErr(fmt.Errorf("oracle11g cursor reference requires cursor pagination on field %q", b.sel.FieldName))
		return "NULL"
	}

	cursorIdx := b.cursorOrderIndex(colName, col)
	if cursorIdx == -1 {
		b.state.setErr(fmt.Errorf("oracle11g unknown cursor column %q on field %q", colName, b.sel.FieldName))
		return "NULL"
	}

	paramName := b.sel.Paging.CursorVar
	if paramName == "" {
		paramName = "cursor"
	}

	argIndex := b.state.registerCursorArg(paramName)
	b.bindIndex++
	b.params = append(b.params, oracle11gParam{
		Type:      "cursor",
		Name:      paramName,
		ArgIndex:  argIndex,
		CursorIdx: cursorIdx,
		ValueType: col.Type,
	})
	return ":" + strconv.Itoa(b.bindIndex)
}

func (b *oracle11gSQLBuilder) cursorOrderIndex(colName string, col sdata.DBColumn) int {
	target := col.Name
	if colName != "" {
		target = colName
	}
	for i, ob := range b.sel.OrderBy {
		if target == ob.Col.Name {
			return i
		}
		if ob.KeyVar != "" && ob.Key != "" && target == (ob.Col.Name+"_"+ob.Key) {
			return i
		}
	}
	return -1
}

func (s *oracle11gCompileState) registerCursorArg(name string) int {
	if name == "" {
		name = "cursor"
	}
	if idx, ok := s.cursorArgIndex[name]; ok {
		return idx
	}
	idx := s.argIndex
	if reg, ok := s.ctx.(ParamRegisterer); ok {
		reg.RegisterParam(Param{Name: name, Type: "text"})
	}
	s.cursorArgIndex[name] = idx
	s.argIndex++
	return idx
}

func (b *oracle11gSQLBuilder) opString(op qcode.ExpOp) (string, bool) {
	switch op {
	case qcode.OpEquals:
		return "=", true
	case qcode.OpNotEquals:
		return "<>", true
	case qcode.OpGreaterOrEquals:
		return ">=", true
	case qcode.OpLesserOrEquals:
		return "<=", true
	case qcode.OpGreaterThan:
		return ">", true
	case qcode.OpLesserThan:
		return "<", true
	case qcode.OpIn:
		return "IN", true
	case qcode.OpNotIn:
		return "NOT IN", true
	case qcode.OpLike:
		return "LIKE", true
	case qcode.OpNotLike:
		return "NOT LIKE", true
	default:
		return "", false
	}
}

func (b *oracle11gSQLBuilder) quote(s string) string {
	return `"` + strings.ToUpper(s) + `"`
}

func (b *oracle11gSQLBuilder) dbColName(col sdata.DBColumn) string {
	if col.OrigName != "" {
		return col.OrigName
	}
	return col.Name
}

// ---------------------------------------------------------------------------
// Mutation instruction types
// ---------------------------------------------------------------------------

type oracle11gMutationInstruction struct {
	Operation    string                  `json:"operation"`
	Error        string                  `json:"error,omitempty"`
	QueryName    string                  `json:"query_name,omitempty"`
	MutationType string                  `json:"mutation_type"`
	Steps        []oracle11gMutationStep `json:"steps"`
	Queries      []oracle11gQuery        `json:"queries,omitempty"`
}

type oracle11gMutationStep struct {
	ID             int32             `json:"id"`
	Type           string            `json:"type"`
	FieldName      string            `json:"field_name"`
	Table          string            `json:"table"`
	Schema         string            `json:"schema,omitempty"`
	MutationSQL    string            `json:"mutation_sql"`
	MutationParams []oracle11gParam  `json:"mutation_params"`
	ReturnSQL      string            `json:"return_sql"`
	ReturnParams   []oracle11gParam  `json:"return_params"`
	Columns        []oracle11gColumn `json:"columns"`
	PKCol          string            `json:"pk_col"`
	PKParamIndex   int               `json:"pk_param_index"`
	Singular       bool              `json:"singular"`
	Children       []int32           `json:"children,omitempty"`
}

// ---------------------------------------------------------------------------
// CompileFullMutation — implements dialect.FullMutationCompiler
// ---------------------------------------------------------------------------

func (d *Oracle11gDialect) CompileFullMutation(ctx Context, qc *qcode.QCode) bool {
	mutType := ""
	switch qc.SType {
	case qcode.QTInsert:
		mutType = "insert"
	case qcode.QTUpdate:
		mutType = "update"
	case qcode.QTDelete:
		mutType = "delete"
	case qcode.QTUpsert:
		mutType = "upsert"
	default:
		mutType = "unknown"
	}

	inst := oracle11gMutationInstruction{
		Operation:    "oracle11g_mutation",
		QueryName:    qc.Name,
		MutationType: mutType,
	}

	state := oracle11gCompileState{
		ctx:            ctx,
		qc:             qc,
		dialect:        d,
		cursorArgIndex: make(map[string]int),
	}

	// Topologically sort the mutations using DependsOn.
	ordered := sortMutations(qc.Mutates)

	// Map from mutation ID to its compiled step index for resolving step_result.
	idToStep := make(map[int32]int, len(ordered))

	for _, mid := range ordered {
		m := qc.Mutates[mid]
		if m.Type == qcode.MTNone || m.Type == qcode.MTKeyword {
			continue
		}

		step := state.compileMutationStep(m, idToStep)
		idToStep[m.ID] = len(inst.Steps)
		inst.Steps = append(inst.Steps, step)
	}

	// Compile child queries for the return data.
	// Walk the first root mutation's SelID to find associated selects with children.
	if len(qc.Mutates) > 0 {
		rootM := qc.Mutates[0]
		if int(rootM.SelID) < len(qc.Selects) {
			sel := &qc.Selects[rootM.SelID]
			for _, childID := range sel.Children {
				child := &qc.Selects[childID]
				if child.SkipRender == qcode.SkipTypeDrop ||
					child.SkipRender == qcode.SkipTypeRemote ||
					child.SkipRender == qcode.SkipTypeDatabaseJoin ||
					child.Field.SkipRender == qcode.SkipTypeDrop ||
					child.Field.SkipRender == qcode.SkipTypeRemote ||
					child.Field.SkipRender == qcode.SkipTypeDatabaseJoin {
					continue
				}
				state.compileTree(child, &inst.Queries)
			}
		}
	}

	if state.err != nil {
		inst.Error = state.err.Error()
		inst.Steps = nil
		inst.Queries = nil
	}

	b, err := json.Marshal(inst)
	if err != nil {
		fallback := oracle11gMutationInstruction{
			Operation:    "oracle11g_mutation",
			MutationType: mutType,
			Error:        err.Error(),
		}
		b, _ = json.Marshal(fallback)
	}
	ctx.WriteString(string(b))

	return true
}

// sortMutations performs a topological sort of mutations by DependsOn.
func sortMutations(mutates []qcode.Mutate) []int {
	if len(mutates) == 0 {
		return nil
	}
	visited := make(map[int]bool)
	var stack []int

	var visit func(int)
	visit = func(id int) {
		if visited[id] {
			return
		}
		visited[id] = true
		m := mutates[id]
		for depID := range m.DependsOn {
			if int(depID) < len(mutates) {
				visit(int(depID))
			}
		}
		stack = append(stack, id)
	}

	for i := range mutates {
		visit(i)
	}
	return stack
}

// compileMutationStep compiles a single mutation into a step instruction.
func (s *oracle11gCompileState) compileMutationStep(m qcode.Mutate, idToStep map[int32]int) oracle11gMutationStep {
	step := oracle11gMutationStep{
		ID:        m.ID,
		FieldName: m.Key,
		Table:     m.Ti.Name,
		Schema:    m.Ti.Schema,
		Singular:  true,
	}

	switch m.Type {
	case qcode.MTInsert:
		step.Type = "insert"
	case qcode.MTUpdate:
		step.Type = "update"
	case qcode.MTDelete:
		step.Type = "delete"
	case qcode.MTUpsert:
		step.Type = "upsert"
	case qcode.MTConnect:
		step.Type = "connect"
	case qcode.MTDisconnect:
		step.Type = "disconnect"
	default:
		step.Type = "unknown"
	}

	// Determine PK column name.
	pkCol := m.Ti.PrimaryCol.Name
	step.PKCol = pkCol

	// Build mutation SQL + params.
	switch m.Type {
	case qcode.MTInsert:
		s.buildInsertStep(&step, m, idToStep)
	case qcode.MTUpdate:
		s.buildUpdateStep(&step, m, idToStep)
	case qcode.MTDelete:
		s.buildDeleteStep(&step, m)
	case qcode.MTUpsert:
		s.buildUpsertStep(&step, m, idToStep)
	default:
		// Connect/Disconnect are not currently handled by oracle11g mutations.
		s.setErr(fmt.Errorf("oracle11g mutation type %q not yet supported", step.Type))
		return step
	}

	// Build return SQL + columns.
	s.buildReturnQuery(&step, m)

	return step
}

// buildInsertStep builds the INSERT mutation SQL for a step.
func (s *oracle11gCompileState) buildInsertStep(step *oracle11gMutationStep, m qcode.Mutate, idToStep map[int32]int) {
	var buf bytes.Buffer
	buf.WriteString("INSERT INTO ")
	s.writeFullTable(&buf, m.Ti)
	buf.WriteString(" (")

	allCols := s.allMutationCols(m)
	bindIdx := 0

	for i, col := range allCols {
		if i != 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(s.quoteCol(m.Ti, col.Name))
	}

	buf.WriteString(") VALUES (")

	for i, col := range allCols {
		if i != 0 {
			buf.WriteString(", ")
		}
		bindIdx++
		buf.WriteString(":" + strconv.Itoa(bindIdx))

		param := s.makeMutationParam(m, col, idToStep)
		if col.Name == m.Ti.PrimaryCol.Name {
			step.PKParamIndex = len(step.MutationParams)
		}
		step.MutationParams = append(step.MutationParams, param)
	}

	buf.WriteString(")")
	step.MutationSQL = buf.String()
}

// buildUpdateStep builds the UPDATE mutation SQL for a step.
func (s *oracle11gCompileState) buildUpdateStep(step *oracle11gMutationStep, m qcode.Mutate, _ map[int32]int) {
	var buf bytes.Buffer
	buf.WriteString("UPDATE ")
	s.writeFullTable(&buf, m.Ti)
	buf.WriteString(" SET ")

	bindIdx := 0

	// SET clause: only user-provided columns (m.Cols), not RCols or PK.
	setCols := make([]colInfo, 0, len(m.Cols))
	for _, mc := range m.Cols {
		setCols = append(setCols, colInfo{Name: mc.Col.Name, FieldName: mc.FieldName, Col: mc.Col})
	}

	for i, col := range setCols {
		if i != 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(s.quoteCol(m.Ti, col.Name))
		buf.WriteString(" = ")
		bindIdx++
		buf.WriteString(":" + strconv.Itoa(bindIdx))
		step.MutationParams = append(step.MutationParams, oracle11gParam{
			Type:      "arg",
			Name:      col.FieldName,
			ArgIndex:  s.argIndex,
			ValueType: col.Col.Type,
		})
		s.argIndex++
	}

	// WHERE clause.
	buf.WriteString(" WHERE ")

	if int(m.SelID) < len(s.qc.Selects) && s.qc.Selects[m.SelID].Where.Exp != nil {
		expBuilder := oracle11gSQLBuilder{
			state:     s,
			sel:       &s.qc.Selects[m.SelID],
			bindIndex: bindIdx,
		}
		expBuilder.writeExp(&buf, s.qc.Selects[m.SelID].Where.Exp)
		for _, p := range expBuilder.params {
			step.MutationParams = append(step.MutationParams, p)
		}
		bindIdx = expBuilder.bindIndex
	} else {
		// Fallback: WHERE pk = :N
		bindIdx++
		buf.WriteString(s.quoteCol(m.Ti, m.Ti.PrimaryCol.Name))
		buf.WriteString(" = :")
		buf.WriteString(strconv.Itoa(bindIdx))
		step.MutationParams = append(step.MutationParams, oracle11gParam{
			Type:      "arg",
			Name:      m.Ti.PrimaryCol.Name,
			ArgIndex:  s.argIndex,
			ValueType: m.Ti.PrimaryCol.Type,
		})
		s.argIndex++
	}

	// Find PK param index among mutation params.
	step.PKParamIndex = s.findPKParamIndex(step.MutationParams, m.Ti.PrimaryCol.Name)

	step.MutationSQL = buf.String()
}

// buildDeleteStep builds the DELETE mutation SQL for a step.
func (s *oracle11gCompileState) buildDeleteStep(step *oracle11gMutationStep, m qcode.Mutate) {
	var buf bytes.Buffer
	buf.WriteString("DELETE FROM ")
	s.writeFullTable(&buf, m.Ti)
	buf.WriteString(" WHERE ")

	bindIdx := 0

	if int(m.SelID) < len(s.qc.Selects) && s.qc.Selects[m.SelID].Where.Exp != nil {
		expBuilder := oracle11gSQLBuilder{
			state:     s,
			sel:       &s.qc.Selects[m.SelID],
			bindIndex: bindIdx,
		}
		expBuilder.writeExp(&buf, s.qc.Selects[m.SelID].Where.Exp)
		for _, p := range expBuilder.params {
			step.MutationParams = append(step.MutationParams, p)
		}
	} else {
		bindIdx++
		buf.WriteString(s.quoteCol(m.Ti, m.Ti.PrimaryCol.Name))
		buf.WriteString(" = :")
		buf.WriteString(strconv.Itoa(bindIdx))
		step.MutationParams = append(step.MutationParams, oracle11gParam{
			Type:      "arg",
			Name:      m.Ti.PrimaryCol.Name,
			ArgIndex:  s.argIndex,
			ValueType: m.Ti.PrimaryCol.Type,
		})
		s.argIndex++
	}

	step.PKParamIndex = s.findPKParamIndex(step.MutationParams, m.Ti.PrimaryCol.Name)
	step.MutationSQL = buf.String()
}

// buildUpsertStep builds the MERGE INTO (upsert) SQL for a step.
func (s *oracle11gCompileState) buildUpsertStep(step *oracle11gMutationStep, m qcode.Mutate, idToStep map[int32]int) {
	var buf bytes.Buffer

	allCols := s.allMutationCols(m)
	pkName := m.Ti.PrimaryCol.Name

	buf.WriteString("MERGE INTO ")
	s.writeFullTable(&buf, m.Ti)
	buf.WriteString(" t USING (SELECT ")

	bindIdx := 0
	for i, col := range allCols {
		if i != 0 {
			buf.WriteString(", ")
		}
		bindIdx++
		buf.WriteString(":" + strconv.Itoa(bindIdx))
		buf.WriteString(" AS ")
		buf.WriteString(s.quoteCol(m.Ti, col.Name))

		param := s.makeMutationParam(m, col, idToStep)
		if col.Name == pkName {
			step.PKParamIndex = len(step.MutationParams)
		}
		step.MutationParams = append(step.MutationParams, param)
	}

	buf.WriteString(" FROM DUAL) s ON (t.")
	buf.WriteString(s.quoteCol(m.Ti, pkName))
	buf.WriteString(" = s.")
	buf.WriteString(s.quoteCol(m.Ti, pkName))
	buf.WriteString(")")

	// WHEN MATCHED THEN UPDATE SET (non-PK columns).
	nonPK := make([]colInfo, 0, len(allCols))
	for _, col := range allCols {
		if col.Name != pkName {
			nonPK = append(nonPK, col)
		}
	}

	if len(nonPK) > 0 {
		buf.WriteString(" WHEN MATCHED THEN UPDATE SET ")
		for i, col := range nonPK {
			if i != 0 {
				buf.WriteString(", ")
			}
			buf.WriteString("t.")
			buf.WriteString(s.quoteCol(m.Ti, col.Name))
			buf.WriteString(" = s.")
			buf.WriteString(s.quoteCol(m.Ti, col.Name))
		}
	}

	// WHEN NOT MATCHED THEN INSERT.
	buf.WriteString(" WHEN NOT MATCHED THEN INSERT (")
	for i, col := range allCols {
		if i != 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(s.quoteCol(m.Ti, col.Name))
	}
	buf.WriteString(") VALUES (")
	for i, col := range allCols {
		if i != 0 {
			buf.WriteString(", ")
		}
		buf.WriteString("s.")
		buf.WriteString(s.quoteCol(m.Ti, col.Name))
	}
	buf.WriteString(")")

	step.MutationSQL = buf.String()
}

// buildReturnQuery builds the SELECT to fetch the mutated row(s).
func (s *oracle11gCompileState) buildReturnQuery(step *oracle11gMutationStep, m qcode.Mutate) {
	// Determine which columns to return.
	var projections []oracle11gProjection
	if int(m.SelID) < len(s.qc.Selects) {
		sel := &s.qc.Selects[m.SelID]
		builder := oracle11gSQLBuilder{
			state: s,
			sel:   sel,
		}
		builder.initProjections()
		projections = builder.projections
	}

	// If no projections, at least return PK.
	if len(projections) == 0 {
		projections = []oracle11gProjection{{
			Col:       m.Ti.PrimaryCol,
			Source:    m.Ti.PrimaryCol.Name,
			FieldName: m.Ti.PrimaryCol.Name,
			ValueType: m.Ti.PrimaryCol.Type,
		}}
	}

	var buf bytes.Buffer
	buf.WriteString("SELECT ")
	for i, proj := range projections {
		if i != 0 {
			buf.WriteString(", ")
		}
		source := proj.Col.Name
		if proj.Col.OrigName != "" {
			source = proj.Col.OrigName
		}
		buf.WriteString(s.dialect.QuoteColumnIdentifier(m.Ti.Name, source))
		if source != proj.Source {
			buf.WriteString(" AS ")
			buf.WriteString(`"` + strings.ToUpper(proj.Source) + `"`)
		}
	}
	buf.WriteString(" FROM ")
	s.writeFullTable(&buf, m.Ti)
	buf.WriteString(" WHERE ")
	buf.WriteString(s.quoteCol(m.Ti, m.Ti.PrimaryCol.Name))
	buf.WriteString(" = :1")

	step.ReturnSQL = buf.String()
	step.ReturnParams = []oracle11gParam{{
		Type:     "step_pk",
		ParentID: m.ID,
		Column:   m.Ti.PrimaryCol.Name,
	}}

	step.Columns = make([]oracle11gColumn, 0, len(projections))
	for _, proj := range projections {
		step.Columns = append(step.Columns, oracle11gColumn{
			Source:    proj.Source,
			FieldName: proj.FieldName,
			ValueType: proj.ValueType,
			Hidden:    proj.Hidden,
		})
	}

	// Compile child select queries.
	if int(m.SelID) < len(s.qc.Selects) {
		sel := &s.qc.Selects[m.SelID]
		for _, childID := range sel.Children {
			child := &s.qc.Selects[childID]
			if child.SkipRender == qcode.SkipTypeDrop ||
				child.SkipRender == qcode.SkipTypeRemote ||
				child.SkipRender == qcode.SkipTypeDatabaseJoin ||
				child.Field.SkipRender == qcode.SkipTypeDrop ||
				child.Field.SkipRender == qcode.SkipTypeRemote ||
				child.Field.SkipRender == qcode.SkipTypeDatabaseJoin {
				continue
			}
			step.Children = append(step.Children, childID)
		}
	}
}

// ---------------------------------------------------------------------------
// Mutation helper types and methods
// ---------------------------------------------------------------------------

// colInfo represents a column in the mutation context.
type colInfo struct {
	Name      string
	FieldName string
	Col       sdata.DBColumn
	IsRCol    bool   // true if this is a relationship column
	DepID     int32  // dependency mutation ID for relationship columns
}

// allMutationCols returns all columns for the mutation: user columns + relationship columns.
func (s *oracle11gCompileState) allMutationCols(m qcode.Mutate) []colInfo {
	seen := make(map[string]struct{})
	var cols []colInfo

	for _, mc := range m.Cols {
		cols = append(cols, colInfo{Name: mc.Col.Name, FieldName: mc.FieldName, Col: mc.Col})
		seen[mc.Col.Name] = struct{}{}
	}

	for _, rc := range m.RCols {
		if _, ok := seen[rc.Col.Name]; ok {
			continue
		}
		// Find the dependency ID for this relationship column.
		var depID int32 = -1
		for id := range m.DependsOn {
			dep := s.qc.Mutates[id]
			// Match: the RCol's VCol references the dependency table's column.
			if dep.Ti.Name == rc.VCol.Table || dep.Ti.Name == rc.VCol.Name {
				depID = id
				break
			}
		}
		cols = append(cols, colInfo{
			Name:      rc.Col.Name,
			FieldName: rc.Col.Name,
			Col:       rc.Col,
			IsRCol:    true,
			DepID:     depID,
		})
		seen[rc.Col.Name] = struct{}{}
	}

	return cols
}

// makeMutationParam creates the appropriate param for a mutation column.
func (s *oracle11gCompileState) makeMutationParam(_ qcode.Mutate, col colInfo, _ map[int32]int) oracle11gParam {
	if col.IsRCol && col.DepID >= 0 {
		// This column's value comes from a previous step's PK.
		return oracle11gParam{
			Type:      "step_result",
			ParentID:  col.DepID,
			Column:    col.Name,
			ValueType: col.Col.Type,
		}
	}
	// Regular argument from GraphQL variables.
	param := oracle11gParam{
		Type:      "arg",
		Name:      col.FieldName,
		ArgIndex:  s.argIndex,
		ValueType: col.Col.Type,
	}
	if reg, ok := s.ctx.(ParamRegisterer); ok {
		reg.RegisterParam(Param{Name: col.FieldName, Type: col.Col.Type})
	}
	s.argIndex++
	return param
}

// findPKParamIndex finds the index of the PK param in the params slice.
func (s *oracle11gCompileState) findPKParamIndex(params []oracle11gParam, pkName string) int {
	for i, p := range params {
		if p.Name == pkName || p.Column == pkName {
			return i
		}
	}
	return 0
}

// writeFullTable writes the quoted SCHEMA.TABLE identifier.
func (s *oracle11gCompileState) writeFullTable(buf *bytes.Buffer, ti sdata.DBTable) {
	if ti.Schema != "" {
		buf.WriteString(s.dialect.QuoteSchemaIdentifier(ti.Schema))
		buf.WriteString(".")
	}
	buf.WriteString(s.dialect.QuoteTableIdentifier(ti.Name))
}

// quoteCol quotes a column identifier with table context.
func (s *oracle11gCompileState) quoteCol(ti sdata.DBTable, col string) string {
	return s.dialect.QuoteColumnIdentifier(ti.Name, col)
}
