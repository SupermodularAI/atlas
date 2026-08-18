package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrimitivesNullVsEmptyAreDistinct(t *testing.T) {
	withheld := Package{Name: "a", Access: AccessExcluded, Primitives: nil}
	empty := Package{Name: "b", Access: AccessPublic, Primitives: []Primitive{}}

	wb, err := json.Marshal(withheld)
	if err != nil {
		t.Fatal(err)
	}
	eb, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wb), `"primitives":null`) {
		t.Errorf("withheld package must emit primitives:null, got %s", wb)
	}
	if !strings.Contains(string(eb), `"primitives":[]`) {
		t.Errorf("harvested-empty package must emit primitives:[], got %s", eb)
	}
}

func TestSchemaVersionIsEmitted(t *testing.T) {
	a := &Atlas{SchemaVersion: SchemaVersion, Company: "acme"}
	b, err := a.MarshalJSONIndent()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"schemaVersion": 1`) {
		t.Errorf("schemaVersion missing from output: %s", b)
	}
}

func TestOmittedOptionalFieldsAreAbsent(t *testing.T) {
	p := Package{Name: "a", Access: AccessPublic, Primitives: []Primitive{}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"reason", "install"} {
		if strings.Contains(string(b), `"`+absent+`"`) {
			t.Errorf("%q should be omitted when empty: %s", absent, b)
		}
	}
}

func TestUnavailableSourceCarriesReason(t *testing.T) {
	s := Source{Name: "x", Kind: "marketplace", Status: StatusUnavailable, Reason: "fetch failed: 404"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"reason":"fetch failed: 404"`) {
		t.Errorf("reason must survive marshalling: %s", b)
	}
}
