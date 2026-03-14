package dialect

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestOracleSetNameMapDropsConflictingNormalizedIdentifiers(t *testing.T) {
	d := &OracleDialect{}
	d.SetNameMap([]sdata.DBTable{
		{
			Name:       "inventory_reports",
			OrigName:   "INVENTORY_REPORTS",
			Schema:     "public",
			OrigSchema: "PUBLIC",
			Columns: []sdata.DBColumn{
				{Name: "a_1_b", OrigName: "A1B"},
			},
		},
		{
			Name:       "inventory_snapshots",
			OrigName:   "INVENTORY_SNAPSHOTS",
			Schema:     "public",
			OrigSchema: "PUBLIC",
			Columns: []sdata.DBColumn{
				{Name: "a_1_b", OrigName: "A1_B"},
			},
		},
	})

	if got := d.QuoteColumnIdentifier("inventory_reports", "a_1_b"); got != `"A1B"` {
		t.Fatalf("QuoteColumnIdentifier(inventory_reports, a_1_b) = %q, want %q", got, `"A1B"`)
	}
	if got := d.QuoteColumnIdentifier("inventory_snapshots", "a_1_b"); got != `"A1_B"` {
		t.Fatalf("QuoteColumnIdentifier(inventory_snapshots, a_1_b) = %q, want %q", got, `"A1_B"`)
	}
	if got := d.QuoteIdentifier("a_1_b"); got != `"A_1_B"` {
		t.Fatalf("QuoteIdentifier() = %q, want generic fallback %q", got, `"A_1_B"`)
	}
}
