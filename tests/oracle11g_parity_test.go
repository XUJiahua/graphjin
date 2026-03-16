package tests_test

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type oracle11gParityStatus string

const (
	oracle11gParityImplemented           oracle11gParityStatus = "implemented"
	oracle11gParityUnsupported           oracle11gParityStatus = "unsupported"
	oracle11gParityGap                   oracle11gParityStatus = "gap"
	oracle11gParityOracleBaselineSkipped oracle11gParityStatus = "oracle-baseline-skipped"
)

type oracle11gParityCase struct {
	Status oracle11gParityStatus
	Reason string
	Run    func(*testing.T)
}

type oracle11gRunSpec struct {
	ctx       context.Context
	configure func(*testing.T, *core.Config)
	useTx     bool
}

type oracle11gRunOption func(*oracle11gRunSpec)

func withOracle11gContext(ctx context.Context) oracle11gRunOption {
	return func(spec *oracle11gRunSpec) {
		spec.ctx = ctx
	}
}

func withOracle11gConfig(fn func(*testing.T, *core.Config)) oracle11gRunOption {
	return func(spec *oracle11gRunSpec) {
		spec.configure = fn
	}
}

func withOracle11gTx() oracle11gRunOption {
	return func(spec *oracle11gRunSpec) {
		spec.useTx = true
	}
}

func oracle11gJSONRunner(
	gql string,
	vars json.RawMessage,
	expected string,
	opts ...oracle11gRunOption,
) func(*testing.T) {
	return func(t *testing.T) {
		requireOracle11g(t)

		spec := oracle11gRunSpec{}
		for _, opt := range opts {
			opt(&spec)
		}

		conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
		if spec.configure != nil {
			spec.configure(t, conf)
		}

		gj, err := core.NewGraphJin(conf, db)
		require.NoError(t, err)

		ctx := spec.ctx
		if ctx == nil {
			ctx = context.Background()
		}

		var res *core.Result
		if spec.useTx {
			tx, err := db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer tx.Rollback() //nolint:errcheck

			res, err = gj.GraphQLTx(ctx, tx, gql, vars, nil)
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
		} else {
			res, err = gj.GraphQL(ctx, gql, vars, nil)
			require.NoError(t, err)
		}

		assert.JSONEq(t, expected, string(res.Data))
	}
}

func oracle11gErrorRunner(
	gql string,
	vars json.RawMessage,
	expectedErr string,
	opts ...oracle11gRunOption,
) func(*testing.T) {
	return func(t *testing.T) {
		requireOracle11g(t)

		spec := oracle11gRunSpec{}
		for _, opt := range opts {
			opt(&spec)
		}

		conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
		if spec.configure != nil {
			spec.configure(t, conf)
		}

		gj, err := core.NewGraphJin(conf, db)
		require.NoError(t, err)

		ctx := spec.ctx
		if ctx == nil {
			ctx = context.Background()
		}

		_, err = gj.GraphQL(ctx, gql, vars, nil)
		require.EqualError(t, err, expectedErr)
	}
}

func oracle11gCursorPageRunner(gql string, vars json.RawMessage, expected string, cursorField string) func(*testing.T) {
	return func(t *testing.T) {
		requireOracle11g(t)

		conf := newConfig(&core.Config{
			DBType:           dbType,
			DisableAllowList: true,
			SecretKey:        "not_a_real_secret",
		})
		gj, err := core.NewGraphJin(conf, db)
		require.NoError(t, err)

		res, err := gj.GraphQL(context.Background(), gql, vars, nil)
		require.NoError(t, err)

		var val map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(res.Data, &val))
		var cursor string
		require.NoError(t, json.Unmarshal(val[cursorField], &cursor))
		require.NotEmpty(t, cursor)

		assert.JSONEq(t, expected, string(val["products"]))
	}
}

func oracle11gCursorPagesRunner(
	gql string,
	cursorVar string,
	expectedPages []string,
) func(*testing.T) {
	return func(t *testing.T) {
		requireOracle11g(t)

		conf := newConfig(&core.Config{
			DBType:           dbType,
			DisableAllowList: true,
			SecretKey:        "not_a_real_secret",
		})
		gj, err := core.NewGraphJin(conf, db)
		require.NoError(t, err)

		var cursor string
		for i, expected := range expectedPages {
			payload := map[string]any{cursorVar: cursor}
			vars, err := json.Marshal(payload)
			require.NoError(t, err)

			res, err := gj.GraphQL(context.Background(), gql, vars, nil)
			require.NoError(t, err)

			var val map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(res.Data, &val))
			require.NoError(t, json.Unmarshal(val["products_cursor"], &cursor))
			require.NotEmptyf(t, cursor, "missing cursor on page %d", i+1)
			assert.JSONEq(t, expected, string(val["products"]))
		}
	}
}

func oracle11gInvalidCursorRunner(gql string) func(*testing.T) {
	return func(t *testing.T) {
		requireOracle11g(t)

		conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
		gj, err := core.NewGraphJin(conf, db)
		require.NoError(t, err)

		_, err = gj.GraphQL(context.Background(), gql, nil, nil)
		require.Error(t, err)
	}
}

func oracle11gSupportedParityCases() map[string]func(*testing.T) {
	userCtx := context.WithValue(context.Background(), core.UserIDKey, 31)
	roleCtx := context.WithValue(context.Background(), core.UserIDKey, 1)

	return map[string]func(*testing.T){
		"Example_query": oracle11gJSONRunner(`
			query {
				products(limit: 3, order_by: { id: asc }) {
					id
					count_likes
					owner {
						id
						fullName: full_name
					}
				}
			}`, nil,
			`{"products":[{"count_likes":null,"id":1,"owner":{"fullName":"User 1","id":1}},{"count_likes":null,"id":2,"owner":{"fullName":"User 2","id":2}},{"count_likes":null,"id":3,"owner":{"fullName":"User 3","id":3}}]}`),

		"Example_queryInTransaction": oracle11gJSONRunner(`
			query {
				products(limit: 3, order_by: { id: asc }) {
					id
					owner {
						id
						fullName: full_name
					}
				}
			}`, nil,
			`{"products":[{"id":1,"owner":{"fullName":"User 1","id":1}},{"id":2,"owner":{"fullName":"User 2","id":2}},{"id":3,"owner":{"fullName":"User 3","id":3}}]}`,
			withOracle11gTx()),

		"Example_queryWithUser": oracle11gJSONRunner(`
			query {
				products(where: { owner_id: { eq: $user_id } }) {
					id
					owner {
						id
					}
				}
			}`, nil,
			`{"products":[{"id":31,"owner":{"id":31}}]}`,
			withOracle11gContext(userCtx)),

		"Example_queryWithWhereNotIsNullAndGreaterThan": oracle11gJSONRunner(`
			query {
				products(
					where: {
						and: [
							{ not: { id: { is_null: true } } },
							{ price: { gt: 10 } }
						]
					}
					limit: 3
					order_by: { id: asc }) {
					id
					name
					price
				}
			}`, nil,
			`{"products":[{"id":1,"name":"Product 1","price":11.5},{"id":2,"name":"Product 2","price":12.5},{"id":3,"name":"Product 3","price":13.5}]}`),

		"Example_queryWithWhereGreaterThanOrLesserThan": oracle11gJSONRunner(`
			query {
				products(
					limit: 3
					order_by: { id: asc }
					where: {
						or: {
							price: { gt: 20 },
							price: { lt: 22 }
						}
					}
				) {
					id
					name
					price
				}
			}`, nil,
			`{"products":[{"id":1,"name":"Product 1","price":11.5},{"id":2,"name":"Product 2","price":12.5},{"id":3,"name":"Product 3","price":13.5}]}`),

		"Example_queryByID": oracle11gJSONRunner(`
			query {
				products(id: $id) {
					id
					name
				}
			}`, json.RawMessage(`{"id":2}`),
			`{"products":{"id":2,"name":"Product 2"}}`),

		"Example_queryParentsWithChildren": TestOracle11gNestedQuery,
		"Example_queryChildrenWithParent": oracle11gJSONRunner(`
			query {
				products(limit: 2, order_by: { id: asc }) {
					name
					price
					owner {
						email
					}
				}
			}`, nil,
			`{"products":[{"name":"Product 1","owner":{"email":"user1@test.com"},"price":11.5},{"name":"Product 2","owner":{"email":"user2@test.com"},"price":12.5}]}`),

		"Example_queryWithAggregationBlockedColumn": oracle11gErrorRunner(`
			query {
				products {
					sum_price
				}
			}`, nil, `db column blocked: price (role: 'anon')`,
			withOracle11gConfig(func(t *testing.T, conf *core.Config) {
				require.NoError(t, conf.AddRoleTable("anon", "products", core.Query{
					Columns: []string{"id", "name"},
				}))
			})),

		"Example_queryWithFunctionsBlocked": oracle11gErrorRunner(`
			query {
				products {
					sum_price
				}
			}`, nil, `all db functions blocked: sum (role: 'anon')`,
			withOracle11gConfig(func(t *testing.T, conf *core.Config) {
				require.NoError(t, conf.AddRoleTable("anon", "products", core.Query{
					DisableFunctions: true,
				}))
			})),

		"Example_queryWithSyntheticTables": oracle11gJSONRunner(`
			query {
				me @object {
					email
				}
			}`, nil,
			`{"me":{"email":"user1@test.com"}}`,
			withOracle11gContext(roleCtx),
			withOracle11gConfig(func(t *testing.T, conf *core.Config) {
				conf.Tables = []core.Table{{Name: "me", Table: "users"}}
				require.NoError(t, conf.AddRoleTable("user", "me", core.Query{
					Filters: []string{`{ id: $user_id }`},
					Limit:   1,
				}))
			})),

		"Example_queryWithFragments1": oracle11gJSONRunner(`
			fragment userFields1 on user {
				id
				email
			}

			query {
				users(order_by: { id: asc }) {
					...userFields2
					stripe_id
					...userFields1
				}
			}

			fragment userFields2 on user {
				full_name
			}`, nil,
			`{"users":[{"email":"user1@test.com","full_name":"User 1","id":1,"stripe_id":"payment_id_1001"},{"email":"user2@test.com","full_name":"User 2","id":2,"stripe_id":"payment_id_1002"}]}`,
			withOracle11gConfig(func(_ *testing.T, conf *core.Config) { conf.DefaultLimit = 2 })),

		"Example_queryWithFragments2": oracle11gJSONRunner(`
			query {
				users(order_by: { id: asc }) {
					...userFields2
					stripe_id
					...userFields1
				}
			}

			fragment userFields1 on user {
				id
				email
			}

			fragment userFields2 on user {
				full_name
			}`, nil,
			`{"users":[{"email":"user1@test.com","full_name":"User 1","id":1,"stripe_id":"payment_id_1001"},{"email":"user2@test.com","full_name":"User 2","id":2,"stripe_id":"payment_id_1002"}]}`,
			withOracle11gConfig(func(_ *testing.T, conf *core.Config) { conf.DefaultLimit = 2 })),

		"Example_queryWithFragments3": oracle11gJSONRunner(`
			fragment userFields1 on user {
				id
				email
			}

			fragment userFields2 on user {
				full_name
				...userFields1
			}

			query {
				users(order_by: { id: asc }) {
					...userFields2
					stripe_id
				}
			}`, nil,
			`{"users":[{"email":"user1@test.com","full_name":"User 1","id":1,"stripe_id":"payment_id_1001"},{"email":"user2@test.com","full_name":"User 2","id":2,"stripe_id":"payment_id_1002"}]}`,
			withOracle11gConfig(func(_ *testing.T, conf *core.Config) { conf.DefaultLimit = 2 })),

		"Example_queryWithSkipAndIncludeDirective1": oracle11gJSONRunner(`
			query {
				products(limit: 2, order_by: { id: asc }) @include(ifRole: "user") {
					id
					name
				}
				users(limit: 3, order_by: { id: asc }) @skip(ifRole: "user") {
					id
				}
			}`, nil,
			`{"products":null,"users":[{"id":1},{"id":2},{"id":3}]}`),

		"Example_queryWithAddAndRemoveDirective1": TestOracle11gRootAddRemove,

		"Example_queryWithAddAndRemoveDirective2": oracle11gJSONRunner(`
			query {
				products(limit: 2, order_by: { id: asc }) {
					id
					name @add(ifRole: "user")
				}
				users(limit: 3, order_by: { id: asc }) {
					id @remove(ifRole: "anon")
				}
			}`, nil,
			`{"products":[{"id":1},{"id":2}],"users":[{},{},{}]}`),

		"Example_queryWithRemoteAPIJoin": TestOracle11gRemoteAPIJoin,
		"Example_queryWithCursorPagination1": oracle11gCursorPageRunner(`
			query {
				products(
					where: { id: { lesser_or_equals: 100 } }
					first: 3
					after: $cursor
					order_by: { price: desc }) {
					name
				}
				products_cursor
			}`, json.RawMessage(`{"cursor":null}`),
			`[{"name":"Product 100"},{"name":"Product 99"},{"name":"Product 98"}]`,
			"products_cursor"),

		"Example_queryWithCursorPagination2": oracle11gCursorPagesRunner(`
			query {
				products(
					first: 1
					after: $cursor
					where: { id: { lteq: 100 }}
					order_by: { price: desc }) {
					name
				}
				products_cursor
			}`, "cursor", []string{
			`[{"name":"Product 100"}]`,
			`[{"name":"Product 99"}]`,
			`[{"name":"Product 98"}]`,
			`[{"name":"Product 97"}]`,
			`[{"name":"Product 96"}]`,
			`[{"name":"Product 95"}]`,
			`[{"name":"Product 94"}]`,
			`[{"name":"Product 93"}]`,
			`[{"name":"Product 92"}]`,
			`[{"name":"Product 91"}]`,
			`[{"name":"Product 90"}]`,
			`[{"name":"Product 89"}]`,
			`[{"name":"Product 88"}]`,
			`[{"name":"Product 87"}]`,
			`[{"name":"Product 86"}]`,
			`[{"name":"Product 85"}]`,
			`[{"name":"Product 84"}]`,
			`[{"name":"Product 83"}]`,
			`[{"name":"Product 82"}]`,
			`[{"name":"Product 81"}]`,
			`[{"name":"Product 80"}]`,
			`[{"name":"Product 79"}]`,
			`[{"name":"Product 78"}]`,
			`[{"name":"Product 77"}]`,
			`[{"name":"Product 76"}]`,
		}),

		"Example_queryWithSkippingAuthRequiredSelectors": oracle11gJSONRunner(`
			query {
				products(limit: 2, order_by: { id: asc }) {
					id
					name
					owner(where: { id: { eq: $user_id } }) {
						id
						email
					}
				}
			}`, nil,
			`{"products":[{"id":1,"name":"Product 1","owner":null},{"id":2,"name":"Product 2","owner":null}]}`),

		"Example_queryWithTypename": oracle11gJSONRunner(`
			query getUser {
				__typename
				users(id: 1) {
					id
					email
					__typename
				}
			}`, json.RawMessage(`{"id":2}`),
			`{"__typename":"getUser","users":{"__typename":"users","email":"user1@test.com","id":1}}`),

		"Example_queryWithNamedCursorPagination": oracle11gCursorPageRunner(`
			query {
				products(
					where: { id: { lesser_or_equals: 100 } }
					first: 3
					after: $products_cursor
					order_by: { price: desc }) {
					name
				}
				products_cursor
			}`, json.RawMessage(`{"products_cursor":null}`),
			`[{"name":"Product 100"},{"name":"Product 99"},{"name":"Product 98"}]`,
			"products_cursor"),

		"Example_queryWithNamedCursorPaginationMultiplePages": oracle11gCursorPagesRunner(`
			query {
				products(
					first: 1
					after: $products_cursor
					where: { id: { lteq: 100 }}
					order_by: { price: desc }) {
					name
				}
				products_cursor
			}`, "products_cursor", []string{
			`[{"name":"Product 100"}]`,
			`[{"name":"Product 99"}]`,
			`[{"name":"Product 98"}]`,
		}),

		"Example_queryWithNamedCursorInvalidVariable": oracle11gInvalidCursorRunner(`
			query {
				products(
					first: 3
					after: $users_cursor) {
					name
				}
				products_cursor
			}`),

		"Example_queryWithBackwardCompatibleCursor": oracle11gCursorPageRunner(`
			query {
				products(
					where: { id: { lesser_or_equals: 100 } }
					first: 3
					after: $cursor
					order_by: { price: desc }) {
					name
				}
				products_cursor
			}`, json.RawMessage(`{"cursor":null}`),
			`[{"name":"Product 100"},{"name":"Product 99"},{"name":"Product 98"}]`,
			"products_cursor"),

		"Example_queryWithVariableLimit": oracle11gJSONRunner(`
			query {
				products(limit: $limit) {
					id
				}
			}`, json.RawMessage(`{"limit":10}`),
			`{"products":[{"id":1},{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8},{"id":9},{"id":10}]}`),
	}
}

func oracle11gUnsupportedParityReasons() map[string]string {
	reasons := map[string]string{
		"Example_queryJSONPathOperations":                         "oracle11g does not support JSON-path filtering in the read-path compiler",
		"Example_queryJSONPathOperationsAlternativeSyntax":        "oracle11g does not support JSON-path filtering in the read-path compiler",
		"Example_queryWithWhereHasAnyKey":                         "oracle11g does not support JSON key operators in the read-path compiler",
		"Example_queryFunctionWithJsonbParam":                     "oracle11g JSON function-parameter parity is not targeted by the current read-path driver",
		"Example_insertIntoTableAndConnectToRelatedTables":        "oracle11g does not support connect/disconnect mutations",
		"Example_insertIntoRecursiveRelationship":                 "oracle11g does not support recursive relationship mutations",
		"Example_insertIntoRecursiveRelationshipAndConnectTable1": "oracle11g does not support recursive relationship + connect mutations",
		"Example_insertIntoRecursiveRelationshipAndConnectTable2": "oracle11g does not support recursive relationship + connect mutations",
		"Example_updateTableAndConnectToRelatedTables":            "oracle11g does not support connect/disconnect mutations",
		"Example_setArrayColumnToValue":                           "oracle11g does not support array column mutations",
		"Example_setArrayColumnToEmpty":                           "oracle11g does not support array column mutations",
		"Example_subscription":                                    "oracle11g does not support subscriptions in the current driver path",
		"Example_subscriptionWithCursor":                          "oracle11g does not support subscriptions in the current driver path",
	}

	return reasons
}

func oracle11gGapParityReasons() map[string]string {
	return map[string]string{
		"Example_queryWithDynamicOrderBy":                        "dynamic named ORDER BY parity is not covered in oracle11g integration tests yet",
		"Example_queryWithNestedOrderBy":                         "oracle11g related-table ORDER BY parity is not covered yet",
		"Example_queryWithLimitOffsetOrderByDistinctAndWhere":    "oracle11g DISTINCT + paging parity is not covered yet",
		"Example_queryWithOrderByList":                           "oracle11g list-valued bind variables for ORDER BY parity are not covered yet",
		"Example_queryWithWhere1":                                "oracle11g iregex operator parity is not covered yet",
		"Example_queryWithWhereIn":                               "oracle11g list-valued bind variables for IN filters are not covered yet",
		"Example_queryWithWhereOnRelatedTable":                   "oracle11g compiler currently rejects nested path filters in integration coverage",
		"Example_queryWithAlternateFieldNames":                   "oracle11g comments/commenter schema parity is still missing",
		"Example_queryBySearch":                                  "oracle11g text-search parity is still missing",
		"Example_queryManyToManyViaJoinTable1":                   "oracle11g join-table relationship parity is still missing",
		"Example_queryManyToManyViaJoinTable2":                   "oracle11g join-table relationship parity is still missing",
		"Example_queryManyToManyViaJoinTable3":                   "oracle11g graph join-table parity is still missing",
		"Example_queryBlockWithRoles":                            "oracle11g roles_query parity is still missing for context-backed user variables",
		"Example_queryWithAggregation":                           "oracle11g aggregate projection parity is still missing",
		"Example_queryWithMultipleTopLevelTables":                "oracle11g multi-root parity is still missing purchases schema coverage",
		"Example_queryWithUnionForPolymorphicRelationships":      "oracle11g polymorphic relationship parity is still missing",
		"Example_queryWithSkipAndIncludeIfArg":                   "oracle11g field-level includeIf/skipIf parity is still missing",
		"Example_queryWithSkipAndIncludeDirective2":              "oracle11g field-level role directives parity is still missing",
		"Example_queryWithSkipAndIncludeDirective3":              "oracle11g table-level ifVar directives parity is still missing",
		"Example_queryWithSkipAndIncludeDirective4":              "oracle11g field-level ifVar directives parity is still missing",
		"Example_queryViewByID":                                  "oracle11g view parity is still missing",
		"Example_queryWithView":                                  "oracle11g view parity is still missing",
		"Example_queryWithRecursiveRelationship1":                "oracle11g recursive relationship parity is still missing",
		"Example_queryWithRecursiveRelationship2":                "oracle11g recursive relationship parity is still missing",
		"Example_queryWithRecursiveRelationshipAndAggregations":  "oracle11g recursive aggregation parity is still missing",
		"Example_queryWithCamelToSnakeCase":                      "oracle11g camelCase + view parity is still missing",
		"Example_queryWithGeoFilter":                             "oracle11g geospatial parity is still missing",
		"Example_queryWithGeoContains":                           "oracle11g geospatial parity is still missing",
		"Example_queryWithWhereInWithVariableArrayColumn":        "oracle11g array-column parity is still missing",
		"Example_queryWithWhereInWithStaticArrayColumn":          "oracle11g array-column parity is still missing",
		"Example_queryWithWhereInWithVariableNumericArrayColumn": "oracle11g array-column parity is still missing",
		"Example_queryWithWhereInWithStaticNumericArrayColumn":   "oracle11g array-column parity is still missing",
		"Example_queryWithFunctionFields":                        "oracle11g custom database function parity is still missing",
		"Example_queryWithFunctionFieldsArgList":                 "oracle11g custom database function parity is still missing",
		"Example_queryWithFunctionReturingTables":                "oracle11g table-returning function parity is still missing",
		"Example_queryWithFunctionReturingTablesWithArgs":        "oracle11g table-returning function parity is still missing",
		"Example_queryWithFunctionReturingTablesWithNamedArgs":   "oracle11g table-returning function parity is still missing",
		"Example_queryWithFunctionReturingUserDefinedTypes":      "oracle11g user-defined type function parity is still missing",
		"Example_queryWithFunctionAndDirectives":                 "oracle11g custom database function parity is still missing",
		"Example_queryWithFunctionsWithWhere":                    "oracle11g aggregate function parity is still missing",
		"Example_queryWithVariables":                             "oracle11g config-backed default variable parity is still missing",
		"Example_queryWithVariablesDefaultValue":                 "oracle11g config-backed default variable parity is still missing",
		"Example_insert":                                         "oracle11g mutation parity runner not yet wired",
		"Example_insertWithTransaction":                          "oracle11g mutation parity runner not yet wired",
		"Example_insertInlineWithValidation":                     "oracle11g mutation parity runner not yet wired",
		"Example_insertInlineBulk":                               "oracle11g mutation parity runner not yet wired",
		"Example_insertWithPresets":                              "oracle11g mutation parity runner not yet wired",
		"Example_insertBulk":                                     "oracle11g mutation parity runner not yet wired",
		"Example_insertIntoMultipleRelatedTables":                "oracle11g mutation parity runner not yet wired",
		"Example_insertIntoTableAndRelatedTable1":                "oracle11g mutation parity runner not yet wired",
		"Example_insertIntoTableAndRelatedTable2":                "oracle11g mutation parity runner not yet wired",
		"Example_insertIntoTableBulkInsertIntoRelatedTable":      "oracle11g mutation parity runner not yet wired",
		"Example_insertWithCamelToSnakeCase":                     "oracle11g mutation parity runner not yet wired",
		"Example_update":                                         "oracle11g mutation parity runner not yet wired",
		"Example_updateMultipleRelatedTables1":                   "oracle11g mutation parity runner not yet wired",
		"Example_updateTableAndRelatedTable":                     "oracle11g mutation parity runner not yet wired",
	}
}

func oracle11gOracleBaselineSkippedReasons() map[string]string {
	return map[string]string{
		"Example_queryWithNestedIndependentCursors": "the shared Oracle example already short-circuits outside postgres/mysql and does not exercise Oracle directly",
	}
}

func oracle11gParityCatalog() map[string]oracle11gParityCase {
	cases := make(map[string]oracle11gParityCase)

	for name, run := range oracle11gSupportedParityCases() {
		cases[name] = oracle11gParityCase{Status: oracle11gParityImplemented, Run: run}
	}
	for name, reason := range oracle11gUnsupportedParityReasons() {
		cases[name] = oracle11gParityCase{Status: oracle11gParityUnsupported, Reason: reason}
	}
	for name, reason := range oracle11gGapParityReasons() {
		cases[name] = oracle11gParityCase{Status: oracle11gParityGap, Reason: reason}
	}
	for name, reason := range oracle11gOracleBaselineSkippedReasons() {
		cases[name] = oracle11gParityCase{Status: oracle11gParityOracleBaselineSkipped, Reason: reason}
	}

	return cases
}

func collectOracleExampleNames(t *testing.T) []string {
	t.Helper()

	files := []string{
		"./geo_test.go",
		"./insert_test.go",
		"./query_pg_test.go",
		"./query_test.go",
		"./subs_test.go",
		"./update_test.go",
	}

	fset := token.NewFileSet()
	var names []string

	for _, file := range files {
		parsed, err := parser.ParseFile(fset, filepath.Clean(file), nil, parser.ParseComments)
		require.NoError(t, err)

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				continue
			}
			if fn.Type.Params != nil && len(fn.Type.Params.List) != 0 {
				continue
			}
			if fn.Type.Results != nil && len(fn.Type.Results.List) != 0 {
				continue
			}
			if len(fn.Name.Name) > len("Example_") && fn.Name.Name[:len("Example_")] == "Example_" {
				names = append(names, fn.Name.Name)
			}
		}
	}

	sort.Strings(names)
	return names
}

func TestOracle11gParityCatalogCompleteness(t *testing.T) {
	examples := collectOracleExampleNames(t)
	catalog := oracle11gParityCatalog()

	var missing []string
	for _, name := range examples {
		if _, ok := catalog[name]; !ok {
			missing = append(missing, name)
		}
	}
	require.Emptyf(t, missing, "oracle11g parity catalog missing cases: %v", missing)

	expectedSet := make(map[string]struct{}, len(examples))
	for _, name := range examples {
		expectedSet[name] = struct{}{}
	}

	var stale []string
	for name := range catalog {
		if _, ok := expectedSet[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	require.Emptyf(t, stale, "oracle11g parity catalog has stale cases: %v", stale)
}

func TestOracle11gOracleParity(t *testing.T) {
	requireOracle11g(t)

	examples := collectOracleExampleNames(t)
	catalog := oracle11gParityCatalog()

	for _, name := range examples {
		tc := catalog[name]
		t.Run(name, func(t *testing.T) {
			switch tc.Status {
			case oracle11gParityImplemented:
				require.NotNilf(t, tc.Run, "implemented parity case %s is missing a runner", name)
				tc.Run(t)
			case oracle11gParityUnsupported:
				t.Skip("oracle11g unsupported: " + tc.Reason)
			case oracle11gParityGap:
				t.Skip("oracle11g parity gap: " + tc.Reason)
			case oracle11gParityOracleBaselineSkipped:
				t.Skip("oracle oracle baseline already skips this case: " + tc.Reason)
			default:
				t.Fatalf("unknown oracle11g parity status for %s: %q", name, tc.Status)
			}
		})
	}
}

func TestOracle11gUnsupportedJSONVirtualTableParity(t *testing.T) {
	requireOracle11g(t)
	t.Skip("oracle11g unsupported: JSON virtual table parity is not implemented for the read-path driver")
}

func TestOracle11gParitySummary(t *testing.T) {
	catalog := oracle11gParityCatalog()

	var (
		implemented int
		unsupported int
		gaps        int
		baseline    int
	)

	for _, tc := range catalog {
		switch tc.Status {
		case oracle11gParityImplemented:
			implemented++
		case oracle11gParityUnsupported:
			unsupported++
		case oracle11gParityGap:
			gaps++
		case oracle11gParityOracleBaselineSkipped:
			baseline++
		}
	}

	t.Logf("oracle11g parity catalog: implemented=%d unsupported=%d gaps=%d oracle-baseline-skipped=%d", implemented, unsupported, gaps, baseline)
}
