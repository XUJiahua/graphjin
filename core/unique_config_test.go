package core

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestUpdateTableMarksUniqueColumns(t *testing.T) {
	di := sdata.GetTestDBInfo()

	err := updateTable(&Config{}, di, Table{
		Name: "users",
		Columns: []Column{
			{Name: "email", Unique: true},
		},
	})
	if err != nil {
		t.Fatalf("updateTable() error: %v", err)
	}

	col, err := di.GetColumn("public", "users", "email")
	if err != nil {
		t.Fatalf("GetColumn() error: %v", err)
	}
	if !col.UniqueKey {
		t.Fatal("expected config Unique override to mark column as unique")
	}
}
