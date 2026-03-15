package oracle11gdriver

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	go_ora "github.com/sijms/go-ora/v2"
)

type instruction struct {
	Operation     string      `json:"operation"`
	Error         string      `json:"error,omitempty"`
	QueryName     string      `json:"query_name,omitempty"`
	QueryTypename string      `json:"query_typename,omitempty"`
	Queries       []queryPlan `json:"queries,omitempty"`
}

// Keep this instruction schema in sync with core/internal/dialect/oracle11g.go.
type queryPlan struct {
	ID        int32         `json:"id"`
	FieldName string        `json:"field_name"`
	Table     string        `json:"table"`
	Schema    string        `json:"schema,omitempty"`
	SQL       string        `json:"sql,omitempty"`
	Params    []queryParam  `json:"params,omitempty"`
	Columns   []queryColumn `json:"columns,omitempty"`
	Cursor    *queryCursor  `json:"cursor,omitempty"`
	Singular  bool          `json:"singular"`
	Typename  bool          `json:"typename,omitempty"`
	Null      bool          `json:"null,omitempty"`
	Children  []int32       `json:"children,omitempty"`
}

type queryParam struct {
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	ArgIndex  int    `json:"arg_index,omitempty"`
	ParentID  int32  `json:"parent_id,omitempty"`
	Column    string `json:"column,omitempty"`
	CursorIdx int    `json:"cursor_idx,omitempty"`
	ValueType string `json:"value_type,omitempty"`
}

type queryColumn struct {
	Source    string `json:"source"`
	FieldName string `json:"field_name,omitempty"`
	ValueType string `json:"value_type,omitempty"`
	Hidden    bool   `json:"hidden,omitempty"`
}

type queryCursor struct {
	ParamName string              `json:"param_name,omitempty"`
	Prefix    string              `json:"prefix,omitempty"`
	SelID     int32               `json:"sel_id,omitempty"`
	OrderBy   []queryCursorColumn `json:"order_by,omitempty"`
}

type queryCursorColumn struct {
	Source    string `json:"source"`
	ValueType string `json:"value_type,omitempty"`
}

type rowData struct {
	Values map[string]any
	Cols   map[string]any
}

type queryValue struct {
	Data      any
	Cursor    any
	HasCursor bool
}

func (c *Conn) executeInstructions(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	inst, err := parseInstruction(query)
	if err != nil {
		return nil, err
	}
	if inst.Error != "" {
		return nil, fmt.Errorf("oracle11gdriver: %s", inst.Error)
	}
	if inst.Operation == "oracle11g_mutation" {
		return c.executeMutationInstructions(ctx, query, args)
	}
	if inst.Operation != "oracle11g_query" {
		return nil, fmt.Errorf("oracle11gdriver: unsupported operation %q", inst.Operation)
	}

	plansByID := make(map[int32]*queryPlan, len(inst.Queries))
	childIDs := make(map[int32]struct{})
	for i := range inst.Queries {
		plan := &inst.Queries[i]
		plansByID[plan.ID] = plan
		for _, childID := range plan.Children {
			childIDs[childID] = struct{}{}
		}
	}

	rootPlans := make([]*queryPlan, 0, len(inst.Queries))
	for i := range inst.Queries {
		plan := &inst.Queries[i]
		if _, ok := childIDs[plan.ID]; !ok {
			rootPlans = append(rootPlans, plan)
		}
	}

	positionalArgs := make([]any, len(args))
	for _, arg := range args {
		if arg.Ordinal > 0 && arg.Ordinal <= len(args) {
			positionalArgs[arg.Ordinal-1] = arg.Value
		}
	}

	payload := make(map[string]any, (len(rootPlans)*2)+1)
	if inst.QueryTypename != "" {
		payload["__typename"] = inst.QueryTypename
	}

	for _, plan := range rootPlans {
		value, err := c.executeQueryTree(ctx, plan, plansByID, positionalArgs, nil)
		if err != nil {
			return nil, err
		}
		payload[plan.FieldName] = value.Data
		if value.HasCursor {
			payload[plan.FieldName+"_cursor"] = value.Cursor
		}
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("oracle11gdriver: marshal response: %w", err)
	}

	return NewSingleValueRows(b, []string{"__root"}), nil
}

func parseInstruction(query string) (*instruction, error) {
	var inst instruction
	if err := json.Unmarshal([]byte(query), &inst); err != nil {
		return nil, fmt.Errorf("oracle11gdriver: invalid instruction set: %w", err)
	}
	return &inst, nil
}

func (c *Conn) executeQueryTree(
	ctx context.Context,
	plan *queryPlan,
	plansByID map[int32]*queryPlan,
	args []any,
	parent *rowData,
) (queryValue, error) {
	if plan.Null {
		return queryValue{}, nil
	}

	rows, err := c.fetchRows(ctx, plan, args, parent)
	if err != nil {
		return queryValue{}, err
	}

	for _, row := range rows {
		if plan.Typename {
			row.Values["__typename"] = plan.Table
		}

		for _, childID := range plan.Children {
			child := plansByID[childID]
			if child == nil {
				return queryValue{}, fmt.Errorf("oracle11gdriver: unknown child query id %d", childID)
			}
			value, err := c.executeQueryTree(ctx, child, plansByID, args, row)
			if err != nil {
				return queryValue{}, err
			}
			row.Values[child.FieldName] = value.Data
			if value.HasCursor {
				row.Values[child.FieldName+"_cursor"] = value.Cursor
			}
		}
	}

	out := queryValue{
		Cursor:    buildCursorValue(plan.Cursor, rows),
		HasCursor: plan.Cursor != nil,
	}

	if plan.Singular {
		if len(rows) == 0 {
			return out, nil
		}
		out.Data = rows[0].Values
		return out, nil
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Values)
	}
	out.Data = result
	return out, nil
}

func (c *Conn) fetchRows(
	ctx context.Context,
	plan *queryPlan,
	args []any,
	parent *rowData,
) ([]*rowData, error) {
	bindArgs := make([]driver.NamedValue, 0, len(plan.Params))
	for i, param := range plan.Params {
		value, skip, err := resolveParam(param, args, parent, plan.Cursor)
		if err != nil {
			return nil, err
		}
		if skip {
			return nil, nil
		}
		bindArgs = append(bindArgs, driver.NamedValue{
			Ordinal: i + 1,
			Value:   value,
		})
	}

	rows, err := c.queryBase(ctx, plan.SQL, bindArgs)
	if err != nil {
		return nil, fmt.Errorf("oracle11gdriver: query %q failed: %w", plan.FieldName, err)
	}
	defer rows.Close() //nolint:errcheck

	colCount := len(plan.Columns)
	if colCount == 0 {
		colCount = len(rows.Columns())
	}

	results := make([]*rowData, 0, 8)
	for {
		dest := make([]driver.Value, colCount)
		if err := rows.Next(dest); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("oracle11gdriver: scan %q failed: %w", plan.FieldName, err)
		}

		row := &rowData{
			Values: make(map[string]any),
			Cols:   make(map[string]any),
		}

		for i := 0; i < len(plan.Columns) && i < len(dest); i++ {
			col := plan.Columns[i]
			jsonValue, bindValue := normalizeRowValue(dest[i], col.ValueType)
			row.Cols[col.Source] = bindValue

			if col.Hidden {
				continue
			}

			fieldName := col.FieldName
			if fieldName == "" {
				fieldName = col.Source
			}
			row.Values[fieldName] = jsonValue
		}

		results = append(results, row)
	}

	return results, nil
}

func resolveParam(param queryParam, args []any, parent *rowData, cursor *queryCursor) (any, bool, error) {
	switch param.Type {
	case "arg":
		if param.ArgIndex < 0 || param.ArgIndex >= len(args) {
			return nil, false, fmt.Errorf("oracle11gdriver: missing argument %q at index %d", param.Name, param.ArgIndex)
		}
		return args[param.ArgIndex], false, nil

	case "parent":
		if parent == nil {
			return nil, true, nil
		}
		value, ok := parent.Cols[param.Column]
		if !ok || value == nil {
			return nil, true, nil
		}
		return value, false, nil

	case "cursor":
		if param.ArgIndex < 0 || param.ArgIndex >= len(args) {
			return nil, false, nil
		}
		value, err := resolveCursorParam(cursor, param, args[param.ArgIndex])
		if err != nil {
			return nil, false, err
		}
		return value, false, nil

	default:
		return nil, false, fmt.Errorf("oracle11gdriver: unsupported param type %q", param.Type)
	}
}

func normalizeValue(value any, valueType string) any {
	jsonValue, _ := normalizeRowValue(value, valueType)
	return jsonValue
}

func normalizeRowValue(value any, valueType string) (any, any) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case string:
		return normalizeStringValue(v, valueType), normalizeBindStringValue(v, valueType)
	case bool, float32, float64, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return v, v
	case []byte:
		s := string(v)
		return normalizeStringValue(s, valueType), normalizeBindStringValue(s, valueType)
	case time.Time:
		return normalizeTimeValue(v, valueType), v
	case go_ora.TimeStamp:
		t := time.Time(v)
		return normalizeTimeValue(t, valueType), t
	case go_ora.TimeStampTZ:
		t := time.Time(v)
		return normalizeTimeValue(t, valueType), t
	case go_ora.NVarChar:
		s := string(v)
		return normalizeStringValue(s, valueType), s
	case go_ora.NullNVarChar:
		if v.Valid {
			s := string(v.NVarChar)
			return normalizeStringValue(s, valueType), s
		}
		return nil, nil
	case go_ora.Number:
		str, err := (&v).String()
		if err != nil {
			s := fmt.Sprintf("%v", value)
			return s, s
		}
		return parseNumber(str), v
	case *go_ora.Number:
		if v == nil {
			return nil, nil
		}
		str, err := v.String()
		if err != nil {
			s := fmt.Sprintf("%v", value)
			return s, s
		}
		return parseNumber(str), *v
	}

	if valuer, ok := value.(driver.Valuer); ok {
		v, err := valuer.Value()
		if err == nil {
			return normalizeRowValue(v, valueType)
		}
	}

	if stringer, ok := value.(interface{ String() (string, error) }); ok {
		str, err := stringer.String()
		if err == nil {
			return normalizeStringValue(str, valueType), normalizeBindStringValue(str, valueType)
		}
	}

	s := fmt.Sprintf("%v", value)
	return s, s
}

func normalizeTimeValue(value time.Time, valueType string) string {
	valueType = strings.ToLower(strings.TrimSpace(valueType))

	switch {
	case strings.Contains(valueType, "timestamp with time zone"),
		strings.Contains(valueType, "timestamp with local time zone"),
		strings.Contains(valueType, "timestamptz"):
		return value.Format(time.RFC3339Nano)

	case strings.Contains(valueType, "timestamp"):
		return value.Format("2006-01-02T15:04:05.999999999")

	case strings.Contains(valueType, "date"):
		return value.Format("2006-01-02T15:04:05")

	default:
		return value.Format(time.RFC3339Nano)
	}
}

func normalizeStringValue(value string, valueType string) any {
	if isNumericType(valueType) {
		return parseNumber(value)
	}
	return value
}

func normalizeBindStringValue(value string, valueType string) any {
	if !isNumericType(valueType) {
		return value
	}
	if num, err := go_ora.NewNumberFromString(strings.TrimSpace(value)); err == nil {
		return num
	}
	return value
}

func isNumericType(valueType string) bool {
	valueType = strings.ToLower(strings.TrimSpace(valueType))
	switch {
	case strings.Contains(valueType, "number"),
		strings.Contains(valueType, "int"),
		strings.Contains(valueType, "float"),
		strings.Contains(valueType, "double"),
		strings.Contains(valueType, "decimal"),
		strings.Contains(valueType, "numeric"):
		return true
	default:
		return false
	}
}

func parseNumber(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}

	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return json.Number(value)
	}
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		return json.Number(strconv.FormatInt(i, 10))
	}

	return value
}

func resolveCursorParam(info *queryCursor, param queryParam, arg any) (any, error) {
	values := parseCursorValues(info, arg)
	if len(values) == 0 || param.CursorIdx < 0 || param.CursorIdx >= len(values) {
		return nil, nil
	}
	return parseCursorBindValue(values[param.CursorIdx], param.ValueType)
}

func parseCursorValues(info *queryCursor, arg any) []string {
	raw := normalizeCursorString(info, arg)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	if len(parts) < 2 {
		return nil
	}
	if info != nil && parts[0] != strconv.Itoa(int(info.SelID)) {
		return nil
	}
	return parts[1:]
}

func normalizeCursorString(info *queryCursor, arg any) string {
	switch v := arg.(type) {
	case nil:
		return ""
	case string:
		return trimCursorPrefix(v, info)
	case []byte:
		return trimCursorPrefix(string(v), info)
	default:
		return trimCursorPrefix(fmt.Sprintf("%v", v), info)
	}
}

func trimCursorPrefix(value string, info *queryCursor) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if info != nil && info.Prefix != "" && strings.HasPrefix(value, info.Prefix) {
		return strings.TrimPrefix(value, info.Prefix)
	}
	return value
}

func parseCursorBindValue(value, valueType string) (any, error) {
	if value == "" {
		return "", nil
	}

	valueType = strings.ToLower(strings.TrimSpace(valueType))

	switch {
	case isNumericType(valueType):
		return normalizeBindStringValue(value, valueType), nil

	case strings.Contains(valueType, "timestamp with time zone"),
		strings.Contains(valueType, "timestamp with local time zone"),
		strings.Contains(valueType, "timestamptz"):
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, fmt.Errorf("oracle11gdriver: invalid timestamp cursor value %q: %w", value, err)
		}
		return t, nil

	case strings.Contains(valueType, "timestamp"):
		t, err := time.Parse("2006-01-02T15:04:05.999999999", value)
		if err != nil {
			return nil, fmt.Errorf("oracle11gdriver: invalid timestamp cursor value %q: %w", value, err)
		}
		return t, nil

	case strings.Contains(valueType, "date"):
		t, err := time.Parse("2006-01-02T15:04:05", value)
		if err != nil {
			return nil, fmt.Errorf("oracle11gdriver: invalid date cursor value %q: %w", value, err)
		}
		return t, nil

	default:
		return value, nil
	}
}

func buildCursorValue(info *queryCursor, rows []*rowData) any {
	if info == nil || len(info.OrderBy) == 0 || len(rows) == 0 {
		return nil
	}

	last := rows[len(rows)-1]
	if last == nil {
		return nil
	}

	parts := make([]string, 0, len(info.OrderBy)+1)
	parts = append(parts, strconv.Itoa(int(info.SelID)))

	for _, col := range info.OrderBy {
		value, ok := last.Cols[col.Source]
		if !ok || value == nil {
			return nil
		}
		parts = append(parts, formatCursorValue(value, col.ValueType))
	}

	return info.Prefix + strings.Join(parts, ",")
}

func formatCursorValue(value any, valueType string) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	case json.Number:
		return v.String()
	case time.Time:
		return normalizeTimeValue(v, valueType)
	case go_ora.TimeStamp:
		return normalizeTimeValue(time.Time(v), valueType)
	case go_ora.TimeStampTZ:
		return normalizeTimeValue(time.Time(v), valueType)
	case go_ora.Number:
		s, err := (&v).String()
		if err == nil {
			return s
		}
	case *go_ora.Number:
		if v == nil {
			return ""
		}
		s, err := v.String()
		if err == nil {
			return s
		}
	}

	if valuer, ok := value.(driver.Valuer); ok {
		v, err := valuer.Value()
		if err == nil {
			return formatCursorValue(v, valueType)
		}
	}

	if stringer, ok := value.(interface{ String() (string, error) }); ok {
		s, err := stringer.String()
		if err == nil {
			return s
		}
	}

	return fmt.Sprintf("%v", value)
}

// ---------------------------------------------------------------------------
// Mutation instruction types (mirror of dialect/oracle11g.go)
// ---------------------------------------------------------------------------

type mutationInstruction struct {
	Operation    string         `json:"operation"`
	Error        string         `json:"error,omitempty"`
	QueryName    string         `json:"query_name,omitempty"`
	MutationType string         `json:"mutation_type"`
	Steps        []mutationStep `json:"steps"`
	Queries      []queryPlan    `json:"queries,omitempty"`
}

type mutationStep struct {
	ID             int32         `json:"id"`
	Type           string        `json:"type"`
	FieldName      string        `json:"field_name"`
	Table          string        `json:"table"`
	Schema         string        `json:"schema,omitempty"`
	MutationSQL    string        `json:"mutation_sql"`
	MutationParams []queryParam  `json:"mutation_params"`
	ReturnSQL      string        `json:"return_sql"`
	ReturnParams   []queryParam  `json:"return_params"`
	Columns        []queryColumn `json:"columns"`
	PKCol          string        `json:"pk_col"`
	PKParamIndex   int           `json:"pk_param_index"`
	Singular       bool          `json:"singular"`
	Children       []int32       `json:"children,omitempty"`
}

// mutationStepResult holds the result of executing one mutation step.
type mutationStepResult struct {
	PKVal any       // captured PK value
	Rows  []*rowData
}

// ---------------------------------------------------------------------------
// executeMutationInstructions — main mutation execution entry point
// ---------------------------------------------------------------------------

func (c *Conn) executeMutationInstructions(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	var inst mutationInstruction
	if err := json.Unmarshal([]byte(query), &inst); err != nil {
		return nil, fmt.Errorf("oracle11gdriver: invalid mutation instruction: %w", err)
	}
	if inst.Error != "" {
		return nil, fmt.Errorf("oracle11gdriver: %s", inst.Error)
	}

	positionalArgs := make([]any, len(args))
	for _, arg := range args {
		if arg.Ordinal > 0 && arg.Ordinal <= len(args) {
			positionalArgs[arg.Ordinal-1] = arg.Value
		}
	}

	// Begin transaction.
	tx, err := c.BeginTx(ctx, driver.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("oracle11gdriver: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Execute mutation steps in order (already topologically sorted by compiler).
	stepResults := make(map[int32]*mutationStepResult)

	// Build query plan maps for child queries.
	plansByID := make(map[int32]*queryPlan, len(inst.Queries))
	for i := range inst.Queries {
		plan := &inst.Queries[i]
		plansByID[plan.ID] = plan
	}

	for i := range inst.Steps {
		step := &inst.Steps[i]
		result, err := c.executeMutationStep(ctx, step, positionalArgs, stepResults, plansByID)
		if err != nil {
			return nil, fmt.Errorf("oracle11gdriver: step %d (%s %s): %w", step.ID, step.Type, step.FieldName, err)
		}
		stepResults[step.ID] = result
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("oracle11gdriver: commit: %w", err)
	}
	committed = true

	// Assemble response from the first mutation step's result.
	payload := make(map[string]any)
	if len(inst.Steps) > 0 {
		firstStep := &inst.Steps[0]
		result := stepResults[firstStep.ID]
		if result != nil && len(result.Rows) > 0 {
			if firstStep.Singular {
				payload[firstStep.FieldName] = result.Rows[0].Values
			} else {
				rows := make([]map[string]any, 0, len(result.Rows))
				for _, row := range result.Rows {
					rows = append(rows, row.Values)
				}
				payload[firstStep.FieldName] = rows
			}
		} else {
			payload[firstStep.FieldName] = nil
		}
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("oracle11gdriver: marshal mutation response: %w", err)
	}

	return NewSingleValueRows(b, []string{"__root"}), nil
}

// executeMutationStep executes a single mutation step.
func (c *Conn) executeMutationStep(
	ctx context.Context,
	step *mutationStep,
	args []any,
	stepResults map[int32]*mutationStepResult,
	plansByID map[int32]*queryPlan,
) (*mutationStepResult, error) {
	result := &mutationStepResult{}

	// Resolve mutation params first (needed for all operations).
	mutBindArgs, err := c.resolveMutationParams(step.MutationParams, args, stepResults)
	if err != nil {
		return nil, fmt.Errorf("resolve mutation params: %w", err)
	}

	// Capture PK value from mutation params.
	if step.PKParamIndex < len(mutBindArgs) {
		result.PKVal = mutBindArgs[step.PKParamIndex].Value
	}

	// For DELETE: execute return query BEFORE the delete to capture the row.
	if step.Type == "delete" {
		// Temporarily register this step's result so the return query can
		// resolve step_pk params that reference this step's own PK.
		stepResults[step.ID] = result

		rows, err := c.executeMutationReturn(ctx, step, args, stepResults)
		if err != nil {
			return nil, fmt.Errorf("pre-delete return query: %w", err)
		}
		result.Rows = rows
	}

	_, err = c.execBase(ctx, step.MutationSQL, mutBindArgs)
	if err != nil {
		return nil, fmt.Errorf("exec %q: %w", step.MutationSQL, err)
	}

	// For non-DELETE: execute return query AFTER the mutation.
	if step.Type != "delete" {
		// Register step result so return query can resolve step_pk params.
		stepResults[step.ID] = result

		rows, err := c.executeMutationReturn(ctx, step, args, stepResults)
		if err != nil {
			return nil, fmt.Errorf("return query: %w", err)
		}
		result.Rows = rows
	}

	// Execute child queries for each returned row.
	if len(step.Children) > 0 && len(result.Rows) > 0 {
		for _, row := range result.Rows {
			for _, childID := range step.Children {
				child := plansByID[childID]
				if child == nil {
					continue
				}
				value, err := c.executeQueryTree(ctx, child, plansByID, args, row)
				if err != nil {
					return nil, fmt.Errorf("child query %d: %w", childID, err)
				}
				row.Values[child.FieldName] = value.Data
				if value.HasCursor {
					row.Values[child.FieldName+"_cursor"] = value.Cursor
				}
			}
		}
	}

	return result, nil
}

// executeMutationReturn executes the return SQL for a mutation step and returns the rows.
func (c *Conn) executeMutationReturn(
	ctx context.Context,
	step *mutationStep,
	args []any,
	stepResults map[int32]*mutationStepResult,
) ([]*rowData, error) {
	if step.ReturnSQL == "" {
		return nil, nil
	}

	returnBindArgs, err := c.resolveMutationParams(step.ReturnParams, args, stepResults)
	if err != nil {
		return nil, err
	}

	rows, err := c.queryBase(ctx, step.ReturnSQL, returnBindArgs)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	colCount := len(step.Columns)
	if colCount == 0 {
		colCount = len(rows.Columns())
	}

	results := make([]*rowData, 0, 4)
	for {
		dest := make([]driver.Value, colCount)
		if err := rows.Next(dest); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		row := &rowData{
			Values: make(map[string]any),
			Cols:   make(map[string]any),
		}

		for i := 0; i < len(step.Columns) && i < len(dest); i++ {
			col := step.Columns[i]
			jsonValue, bindValue := normalizeRowValue(dest[i], col.ValueType)
			row.Cols[col.Source] = bindValue

			if col.Hidden {
				continue
			}

			fieldName := col.FieldName
			if fieldName == "" {
				fieldName = col.Source
			}
			row.Values[fieldName] = jsonValue
		}

		results = append(results, row)
	}

	return results, nil
}

// resolveMutationParams resolves mutation parameters into bind args.
func (c *Conn) resolveMutationParams(
	params []queryParam,
	args []any,
	stepResults map[int32]*mutationStepResult,
) ([]driver.NamedValue, error) {
	bindArgs := make([]driver.NamedValue, 0, len(params))
	for i, param := range params {
		value, err := resolveMutationParam(param, args, stepResults)
		if err != nil {
			return nil, err
		}
		bindArgs = append(bindArgs, driver.NamedValue{
			Ordinal: i + 1,
			Value:   value,
		})
	}
	return bindArgs, nil
}

// resolveMutationParam resolves a single mutation param value.
func resolveMutationParam(param queryParam, args []any, stepResults map[int32]*mutationStepResult) (any, error) {
	switch param.Type {
	case "arg":
		if param.ArgIndex < 0 || param.ArgIndex >= len(args) {
			return nil, fmt.Errorf("oracle11gdriver: missing argument %q at index %d", param.Name, param.ArgIndex)
		}
		return args[param.ArgIndex], nil

	case "step_result":
		sr := stepResults[param.ParentID]
		if sr == nil {
			return nil, nil
		}
		return sr.PKVal, nil

	case "step_pk":
		sr := stepResults[param.ParentID]
		if sr == nil {
			return nil, nil
		}
		return sr.PKVal, nil

	default:
		return nil, fmt.Errorf("oracle11gdriver: unsupported mutation param type %q", param.Type)
	}
}
