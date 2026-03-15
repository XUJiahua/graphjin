package tests_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireOracle11g(t *testing.T) {
	t.Helper()

	if dbType != "oracle11g" {
		t.Skip("oracle11g-only test")
	}
}

func TestOracle11gNestedQuery(t *testing.T) {
	requireOracle11g(t)

	gql := `query {
		users(limit: 2, order_by: { id: asc }) {
			email
			products(limit: 1, order_by: { id: asc }) {
				name
				price
			}
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	require.NoError(t, err)

	assert.JSONEq(t,
		`{"users":[{"email":"user1@test.com","products":[{"name":"Product 1","price":11.5}]},{"email":"user2@test.com","products":[{"name":"Product 2","price":12.5}]}]}`,
		string(res.Data),
	)
}

func TestOracle11gCursorPagination(t *testing.T) {
	requireOracle11g(t)

	gql := `query {
		products(first: 3, after: $cursor, order_by: { price: desc }) {
			name
			price
		}
		products_cursor
	}`

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		SecretKey:        "not_a_real_secret",
	})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)

	type result struct {
		Products json.RawMessage `json:"products"`
		Cursor   string          `json:"products_cursor"`
	}

	var page1 result
	res, err := gj.GraphQL(context.Background(), gql, json.RawMessage(`{"cursor": null}`), nil)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(res.Data, &page1))
	require.NotEmpty(t, page1.Cursor)
	assert.JSONEq(t,
		`[{"name":"Product 100","price":110.5},{"name":"Product 99","price":109.5},{"name":"Product 98","price":108.5}]`,
		string(page1.Products),
	)

	vars, err := json.Marshal(map[string]string{"cursor": page1.Cursor})
	require.NoError(t, err)

	var page2 result
	res, err = gj.GraphQL(context.Background(), gql, vars, nil)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(res.Data, &page2))
	require.NotEmpty(t, page2.Cursor)
	assert.JSONEq(t,
		`[{"name":"Product 97","price":107.5},{"name":"Product 96","price":106.5},{"name":"Product 95","price":105.5}]`,
		string(page2.Products),
	)
}

func TestOracle11gRootAddRemove(t *testing.T) {
	requireOracle11g(t)

	gql := `query {
		products(limit: 2, order_by: { id: asc }) @add(ifRole: "user") {
			id
			name
		}
		users(limit: 2, order_by: { id: asc }) @remove(ifRole: "user") {
			id
			email
		}
	}`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"users":[{"id":1,"email":"user1@test.com"},{"id":2,"email":"user2@test.com"}]}`,
		string(res.Data),
	)

	ctx := context.WithValue(context.Background(), core.UserIDKey, 1)
	res, err = gj.GraphQL(ctx, gql, nil, nil)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"products":[{"id":1,"name":"Product 1"},{"id":2,"name":"Product 2"}]}`,
		string(res.Data),
	)
}

func TestOracle11gRemoteAPIJoin(t *testing.T) {
	requireOracle11g(t)

	gql := `query {
		users(limit: 2, order_by: { id: asc }) {
			email
			payments {
				desc
			}
		}
	}`

	mux := http.NewServeMux()
	mux.HandleFunc("/payments/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/payments/"):]
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"desc":"Payment 1 for %s"},{"desc":"Payment 2 for %s"}]}`, id, id) //nolint:errcheck
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &http.Server{Handler: mux}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("oracle11g remote api test server stopped: %v", err)
		}
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
	for i := 0; i < 100; i++ {
		resp, err := http.Get(baseURL + "/payments/test")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, DefaultLimit: 2})
	conf.Resolvers = []core.ResolverConfig{{
		Name:      "payments",
		Type:      "remote_api",
		Table:     "users",
		Column:    "stripe_id",
		StripPath: "data",
		Props:     core.ResolverProps{"url": baseURL + "/payments/$id"},
	}}

	gj, err := core.NewGraphJin(conf, db)
	require.NoError(t, err)

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	require.NoError(t, err)

	assert.JSONEq(t,
		`{"users":[{"email":"user1@test.com","payments":[{"desc":"Payment 1 for payment_id_1001"},{"desc":"Payment 2 for payment_id_1001"}]},{"email":"user2@test.com","payments":[{"desc":"Payment 1 for payment_id_1002"},{"desc":"Payment 2 for payment_id_1002"}]}]}`,
		string(res.Data),
	)
}

func TestOracle11gDatabaseJoin(t *testing.T) {
	requireOracle11g(t)

	sqliteDB, err := sql.Open("sqlite3_regexp", "file:oracle11g-multidb?mode=memory&cache=shared&_busy_timeout=5000")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })

	for _, stmt := range []string{
		`CREATE TABLE audit_logs (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, action TEXT NOT NULL)`,
		`INSERT INTO audit_logs (id, user_id, action) VALUES (1, 1, 'CREATE')`,
		`INSERT INTO audit_logs (id, user_id, action) VALUES (2, 2, 'UPDATE')`,
		`INSERT INTO audit_logs (id, user_id, action) VALUES (3, 1, 'DELETE')`,
	} {
		_, err = sqliteDB.Exec(stmt)
		require.NoError(t, err)
	}

	gql := `query {
		users(limit: 2, order_by: { id: asc }) {
			email
			latest_audit_log @object {
				action
			}
		}
	}`

	conf := newConfig(&core.Config{
		DBType:           dbType,
		DisableAllowList: true,
		Databases: map[string]core.DatabaseConfig{
			"default": {Type: dbType},
			"sqlite":  {Type: "sqlite"},
		},
		Tables: []core.Table{
			{
				Name:     "users",
				Schema:   "purchase",
				Database: "default",
				Columns: []core.Column{
					{Name: "latest_audit_log_id", ForeignKey: "sqlite:main.audit_logs.id"},
				},
			},
			{Name: "products", Schema: "purchase", Database: "default"},
			{
				Name:     "audit_logs",
				Schema:   "main",
				Database: "sqlite",
			},
		},
	})

	gj, err := core.NewGraphJin(conf, db, core.OptionSetDatabases(map[string]*sql.DB{
		"default": db,
		"sqlite":  sqliteDB,
	}))
	require.NoError(t, err)

	res, err := gj.GraphQL(context.Background(), gql, nil, nil)
	require.NoError(t, err)

	assert.JSONEq(t,
		`{"users":[{"email":"user1@test.com","latest_audit_log":{"action":"CREATE"}},{"email":"user2@test.com","latest_audit_log":{"action":"UPDATE"}}]}`,
		string(res.Data),
	)
}
