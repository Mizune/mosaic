package store

import (
	"reflect"
	"strings"
	"testing"
)

// TestSessionColumnsSync verifies that sessionColumns contains exactly
// the same set of db-tagged fields as the Session struct. This catches
// drift when a field is added to or removed from Session without
// updating the column list.
func TestSessionColumnsSync(t *testing.T) {
	typ := reflect.TypeOf(Session{})
	var dbTags []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("db")
		if tag != "" && tag != "-" {
			dbTags = append(dbTags, tag)
		}
	}

	cols := strings.Split(sessionColumns, ",")
	var colNames []string
	for _, c := range cols {
		c = strings.TrimSpace(c)
		if c != "" {
			colNames = append(colNames, c)
		}
	}

	if len(dbTags) != len(colNames) {
		t.Fatalf("Session struct has %d db-tagged fields but sessionColumns has %d entries\n  struct: %v\n  columns: %v",
			len(dbTags), len(colNames), dbTags, colNames)
	}

	tagSet := make(map[string]bool, len(dbTags))
	for _, tag := range dbTags {
		tagSet[tag] = true
	}
	for _, col := range colNames {
		if !tagSet[col] {
			t.Errorf("sessionColumns contains %q but Session struct has no matching db tag", col)
		}
	}
}
