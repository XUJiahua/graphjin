package serv

import (
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitMssql_BasicConnectionString(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:     "mssql",
				Host:     "localhost",
				Port:     1433,
				User:     "sa",
				Password: "mypassword",
				DBName:   "testdb",
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "sqlserver", dc.driverName)
	assert.Equal(t, "sqlserver://sa:mypassword@localhost:1433?database=testdb", dc.connString)
}

func TestInitMssql_EncryptDisable(t *testing.T) {
	boolFalse := false
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:     "mssql",
				Host:     "localhost",
				Port:     1433,
				User:     "sa",
				Password: "pass",
				DBName:   "testdb",
				Encrypt:  &boolFalse,
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Contains(t, dc.connString, "encrypt=disable")
	assert.Contains(t, dc.connString, "database=testdb")
}

func TestInitMssql_EncryptTrue(t *testing.T) {
	boolTrue := true
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:     "mssql",
				Host:     "localhost",
				Port:     1433,
				User:     "sa",
				Password: "pass",
				Encrypt:  &boolTrue,
			},
		},
	}

	dc, err := initMssql(conf, false, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Contains(t, dc.connString, "encrypt=true")
}

func TestInitMssql_TrustServerCertificate(t *testing.T) {
	boolFalse := false
	boolTrue := true
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:                   "mssql",
				Host:                   "localhost",
				Port:                   1433,
				User:                   "sa",
				Password:               "pass",
				DBName:                 "testdb",
				Encrypt:                &boolFalse,
				TrustServerCertificate: &boolTrue,
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Contains(t, dc.connString, "encrypt=disable")
	assert.Contains(t, dc.connString, "trustservercertificate=true")
}

func TestInitMssql_SpecialCharsInPassword(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:     "mssql",
				Host:     "localhost",
				Port:     1433,
				User:     "sa",
				Password: "GraphJin!Passw0rd",
				DBName:   "testdb",
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Contains(t, dc.connString, "GraphJin%21Passw0rd")
	assert.NotContains(t, dc.connString, "GraphJin!Passw0rd")
}

func TestInitMssql_ConnStringUsedAsIs(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:       "mssql",
				ConnString: "sqlserver://custom:conn@host:1433",
				DBName:     "testdb",
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "sqlserver://custom:conn@host:1433?database=testdb", dc.connString)
}

func TestInitMssql_DefaultPort(t *testing.T) {
	tests := []struct {
		name string
		port uint16
	}{
		{"port zero defaults to 1433", 0},
		{"postgres port on non-postgres defaults to 1433", 5432},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := &Config{
				Serv: Serv{
					DB: Database{
						Type:     "mssql",
						Host:     "localhost",
						Port:     tt.port,
						User:     "sa",
						Password: "pass",
					},
				},
			}

			dc, err := initMssql(conf, false, false, core.NewOsFS(""))
			require.NoError(t, err)
			assert.Contains(t, dc.connString, "localhost:1433")
		})
	}
}

func TestInitMssql_AllOptions(t *testing.T) {
	boolFalse := false
	boolTrue := true
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:                   "mssql",
				Host:                   "mssqlhost",
				Port:                   1433,
				User:                   "sa",
				Password:               "GraphJin!Passw0rd",
				DBName:                 "testdb",
				Encrypt:                &boolFalse,
				TrustServerCertificate: &boolTrue,
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	expected := "sqlserver://sa:GraphJin%21Passw0rd@mssqlhost:1433?database=testdb&encrypt=disable&trustservercertificate=true"
	assert.Equal(t, expected, dc.connString)
}

func TestInitSnowflake_RequiresConnectionString(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type: "snowflake",
			},
		},
	}

	_, err := initSnowflake(conf, true, false, core.NewOsFS(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snowflake requires connection_string")
}

func TestInitSnowflake_UsesConnectionString(t *testing.T) {
	conn := "user:pass@localhost:8080/test_db/public?account=test&protocol=http&warehouse=dummy"
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:       "snowflake",
				ConnString: conn,
			},
		},
	}

	dc, err := initSnowflake(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "snowflake", dc.driverName)
	assert.Equal(t, conn, dc.connString)
}

func TestDetectDBTypePreservesExplicitOracle11g(t *testing.T) {
	conf := &Config{
		Core: core.Config{DBType: "oracle11g"},
		Serv: Serv{
			DB: Database{
				Type:       "oracle11g",
				ConnString: "oracle://user:pass@localhost:1521/XE",
			},
		},
	}

	detectDBType(conf)

	assert.Equal(t, "oracle11g", conf.DBType)
}

func TestInitDBDriver_Oracle11gUsesWrappedDriver(t *testing.T) {
	conf := &Config{
		Core: core.Config{DBType: "oracle11g"},
		Serv: Serv{
			DB: Database{
				Type:       "oracle11g",
				ConnString: "oracle://user:pass@localhost:1521/XE",
			},
		},
	}

	dc, err := initDBDriver(conf, false, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "oracle11g", dc.driverName)
	assert.Equal(t, "oracle://user:pass@localhost:1521/XE", dc.connString)
}

func TestBuildProbeConnString_Oracle11g(t *testing.T) {
	driverName, connString := buildProbeConnString("oracle11g", "localhost", 1521, "", "purchase", "purchase", "tcp", "XE")

	assert.Equal(t, "oracle11g", driverName)
	assert.Equal(t, "oracle://purchase:purchase@localhost:1521/XE", connString)
}

func TestInitDBDriver_DBTypeFallbackToDatabaseType(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:       "snowflake",
				ConnString: "user:pass@localhost:8080/test_db/public?account=test&protocol=http&warehouse=dummy",
			},
		},
	}

	dc, err := initDBDriver(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "snowflake", conf.DBType)
	assert.Equal(t, "snowflake", dc.driverName)
}
