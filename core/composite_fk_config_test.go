package core

import (
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// dt1/dt2 style composite FK setup: dt2 has a 3-column composite FK to dt1.
// Each of the local columns already carries a single-column `related_to`
// pointing at dt1 (which is how the YAML forces the target table).
func newCompositeTestDBInfo() *sdata.DBInfo {
	cols := []sdata.DBColumn{
		// dt1 (parent)
		{Schema: "public", Table: "dt1", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "dt1", Name: "itm", Type: "varchar", NotNull: true},
		{Schema: "public", Table: "dt1", Name: "lnid", Type: "integer", NotNull: true},
		{Schema: "public", Table: "dt1", Name: "sonum", Type: "varchar", NotNull: true},

		// dt2 (child) — each FK column targets the matching dt1 column.
		{Schema: "public", Table: "dt2", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
		{Schema: "public", Table: "dt2", Name: "itm", Type: "varchar", NotNull: true, FKeySchema: "public", FKeyTable: "dt1", FKeyCol: "itm"},
		{Schema: "public", Table: "dt2", Name: "lnid", Type: "integer", NotNull: true, FKeySchema: "public", FKeyTable: "dt1", FKeyCol: "lnid"},
		{Schema: "public", Table: "dt2", Name: "sonum", Type: "varchar", NotNull: true, FKeySchema: "public", FKeyTable: "dt1", FKeyCol: "sonum"},

		// orders (unrelated third table used for negative tests)
		{Schema: "public", Table: "orders", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
	}

	return sdata.NewDBInfo("", 110000, "public", "db", cols, nil, nil)
}

func TestAddCompositeForeignKeys_HappyPath(t *testing.T) {
	di := newCompositeTestDBInfo()

	conf := &Config{
		Tables: []Table{{
			Name: "dt2",
			CompositeForeignKeys: []CompositeForeignKey{{
				Columns:    []string{"itm", "lnid", "sonum"},
				Table:      "dt1",
				RefColumns: []string{"itm", "lnid", "sonum"},
			}},
		}},
	}

	if err := addCompositeForeignKeys(conf, di, ""); err != nil {
		t.Fatalf("addCompositeForeignKeys: %v", err)
	}

	if got := len(di.CompositeFKs); got != 1 {
		t.Fatalf("di.CompositeFKs len = %d, want 1", got)
	}
	cfk := di.CompositeFKs[0]
	if cfk.Table != "dt2" || cfk.FKeyTable != "dt1" {
		t.Fatalf("unexpected (table=%s, fkTable=%s)", cfk.Table, cfk.FKeyTable)
	}
	if got := strings.Join(cfk.LocalCols, ","); got != "itm,lnid,sonum" {
		t.Fatalf("LocalCols = %q, want itm,lnid,sonum", got)
	}
	if got := strings.Join(cfk.FKeyCols, ","); got != "itm,lnid,sonum" {
		t.Fatalf("FKeyCols = %q, want itm,lnid,sonum", got)
	}
	if cfk.ConstraintName == "" {
		t.Fatal("ConstraintName should be generated when Name is empty")
	}
	if !strings.HasPrefix(cfk.ConstraintName, "__gj_yaml_") {
		t.Fatalf("generated ConstraintName %q should start with __gj_yaml_", cfk.ConstraintName)
	}
}

func TestAddCompositeForeignKeys_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfk     CompositeForeignKey
		wantSub string
	}{
		{
			name: "too few columns",
			cfk: CompositeForeignKey{
				Columns:    []string{"itm"},
				Table:      "dt1",
				RefColumns: []string{"itm"},
			},
			wantSub: "at least two local columns",
		},
		{
			name: "length mismatch",
			cfk: CompositeForeignKey{
				Columns:    []string{"itm", "lnid"},
				Table:      "dt1",
				RefColumns: []string{"itm"},
			},
			wantSub: "must have the same length",
		},
		{
			name: "missing target table",
			cfk: CompositeForeignKey{
				Columns:    []string{"itm", "lnid"},
				Table:      "",
				RefColumns: []string{"itm", "lnid"},
			},
			wantSub: "related_to target table is required",
		},
		{
			name: "local column does not exist",
			cfk: CompositeForeignKey{
				Columns:    []string{"itm", "bogus"},
				Table:      "dt1",
				RefColumns: []string{"itm", "lnid"},
			},
			wantSub: "local column 'bogus' not found",
		},
		{
			name: "local column lacks single-column FK",
			cfk: CompositeForeignKey{
				// dt2.id has no FKeyTable set
				Columns:    []string{"itm", "id"},
				Table:      "dt1",
				RefColumns: []string{"itm", "id"},
			},
			wantSub: "must first declare a single-column `related_to`",
		},
		{
			name: "target column not found",
			cfk: CompositeForeignKey{
				Columns:    []string{"itm", "lnid"},
				Table:      "dt1",
				RefColumns: []string{"itm", "bogus"},
			},
			wantSub: "referenced column 'dt1.bogus' not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			di := newCompositeTestDBInfo()
			conf := &Config{
				Tables: []Table{{
					Name:                 "dt2",
					CompositeForeignKeys: []CompositeForeignKey{tc.cfk},
				}},
			}
			err := addCompositeForeignKeys(conf, di, "")
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
			if len(di.CompositeFKs) != 0 {
				t.Fatalf("di.CompositeFKs should be empty on validation failure, got %d", len(di.CompositeFKs))
			}
		})
	}
}

// Integration check: after addCompositeForeignKeys runs, NewDBSchema must
// merge the per-column edges into a single edge with ExtraPairs. This is the
// property that guarantees downstream qcode generates an ANDed WHERE clause.
func TestCompositeForeignKeys_MergesIntoSingleEdgeWithExtraPairs(t *testing.T) {
	di := newCompositeTestDBInfo()
	conf := &Config{
		Tables: []Table{{
			Name: "dt2",
			CompositeForeignKeys: []CompositeForeignKey{{
				Columns:    []string{"itm", "lnid", "sonum"},
				Table:      "dt1",
				RefColumns: []string{"itm", "lnid", "sonum"},
			}},
		}},
	}
	if err := addCompositeForeignKeys(conf, di, ""); err != nil {
		t.Fatalf("addCompositeForeignKeys: %v", err)
	}

	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema: %v", err)
	}

	// Resolve both tables via the schema graph.
	if _, err := schema.Find("public", "dt1"); err != nil {
		t.Fatalf("schema.Find(dt1): %v", err)
	}
	if _, err := schema.Find("public", "dt2"); err != nil {
		t.Fatalf("schema.Find(dt2): %v", err)
	}

	// FindPath from child → parent. With a composite FK the edge merging in
	// sdata.addColumnRels must collapse three parallel edges into ONE edge
	// carrying the remaining two column pairs as ExtraPairs. Without the
	// addCompositeForeignKeys step, three independent edges would exist and
	// FindPath would walk only the first one (zero ExtraPairs).
	paths, err := schema.FindPath("dt2", "dt1", "")
	if err != nil {
		t.Fatalf("schema.FindPath(dt2, dt1): %v", err)
	}
	if got := len(paths); got != 1 {
		t.Fatalf("expected exactly 1 hop from dt2→dt1, got %d", got)
	}
	if got := len(paths[0].ExtraPairs); got != 2 {
		t.Fatalf("expected 2 ExtraPairs on merged edge (itm + 2 composite cols - 1 primary = 2), got %d", got)
	}
}

// Negative control: without composite_related_to, the three per-column
// related_to entries must stay as THREE separate edges — which is exactly
// the bug we're fixing. This test documents the baseline behaviour.
func TestNoCompositeForeignKeys_ParallelEdgesStayParallel(t *testing.T) {
	di := newCompositeTestDBInfo()

	schema, err := sdata.NewDBSchema(di, nil)
	if err != nil {
		t.Fatalf("NewDBSchema: %v", err)
	}

	paths, err := schema.FindPath("dt2", "dt1", "")
	if err != nil {
		t.Fatalf("schema.FindPath(dt2, dt1): %v", err)
	}
	// A non-composite path still resolves to 1 hop, but ExtraPairs must be empty.
	if got := len(paths); got != 1 {
		t.Fatalf("expected 1 hop from dt2→dt1, got %d", got)
	}
	if got := len(paths[0].ExtraPairs); got != 0 {
		t.Fatalf("expected 0 ExtraPairs on non-composite edge, got %d", got)
	}
}
