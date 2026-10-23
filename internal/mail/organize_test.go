package mail

import (
	"encoding/json"
	"reflect"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestChangeValidateRejectsEmptyChange(t *testing.T) {
	if err := (Change{}).Validate(); err == nil {
		t.Error("Validate() = nil, want an error for a change that does nothing")
	}
}

func TestChangeValidateRejectsUnknownFlag(t *testing.T) {
	if err := (Change{Flag: "urgent"}).Validate(); err == nil {
		t.Error("Validate() = nil, want an error for an unknown flag status")
	}
}

func TestChangeValidateRejectsAddAndRemoveOfSameCategory(t *testing.T) {
	c := Change{AddCategories: []string{"Clients"}, RemoveCategories: []string{"clients"}}
	if err := c.Validate(); err == nil {
		t.Error("Validate() = nil, want an error when a category is both added and removed")
	}
}

func TestChangeValidateAcceptsEachKindAlone(t *testing.T) {
	for name, c := range map[string]Change{
		"add":      {AddCategories: []string{"Clients"}},
		"remove":   {RemoveCategories: []string{"Clients"}},
		"clear":    {ClearCategories: true},
		"read":     {Read: boolPtr(true)},
		"unread":   {Read: boolPtr(false)},
		"flag":     {Flag: FlagFlagged},
		"complete": {Flag: FlagComplete},
		"unflag":   {Flag: FlagNone},
	} {
		if err := c.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v, want nil", name, err)
		}
	}
}

func TestMergeCategoriesAddsKeepsOrderAndDedupesCaseInsensitively(t *testing.T) {
	got := MergeCategories([]string{"Clients", "Todo"}, Change{AddCategories: []string{"todo", "Invoices", "Invoices"}})
	want := []string{"Clients", "Todo", "Invoices"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeCategories = %v, want %v", got, want)
	}
}

func TestMergeCategoriesRemovesCaseInsensitively(t *testing.T) {
	got := MergeCategories([]string{"Clients", "Todo"}, Change{RemoveCategories: []string{"TODO", "Missing"}})
	want := []string{"Clients"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeCategories = %v, want %v", got, want)
	}
}

func TestMergeCategoriesClearThenAddSetsExactly(t *testing.T) {
	got := MergeCategories([]string{"Clients", "Todo"}, Change{ClearCategories: true, AddCategories: []string{"Done"}})
	want := []string{"Done"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeCategories = %v, want %v", got, want)
	}
}

func TestMergeCategoriesClearAloneYieldsEmptyNotNil(t *testing.T) {
	got := MergeCategories([]string{"Clients"}, Change{ClearCategories: true})
	if got == nil || len(got) != 0 {
		t.Errorf("MergeCategories = %#v, want an empty, non-nil slice (Graph needs [] to clear)", got)
	}
}

func TestBuildOrganizePatchOnlyIncludesRequestedFields(t *testing.T) {
	payload, err := BuildOrganizePatch(nil, Change{Read: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["isRead"] != true {
		t.Errorf("patch = %s, want only {\"isRead\":true}", payload)
	}
}

func TestBuildOrganizePatchCombinesCategoriesAndFlag(t *testing.T) {
	payload, err := BuildOrganizePatch([]string{"Clients"}, Change{AddCategories: []string{"Todo"}, Flag: FlagFlagged})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"categories":["Clients","Todo"],"flag":{"flagStatus":"flagged"}}`
	if string(payload) != want {
		t.Errorf("patch = %s, want %s", payload, want)
	}
}

func TestBuildOrganizePatchClearSendsEmptyArray(t *testing.T) {
	payload, err := BuildOrganizePatch([]string{"Clients"}, Change{ClearCategories: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"categories":[]}` {
		t.Errorf("patch = %s, want {\"categories\":[]}", payload)
	}
}

func TestBuildOrganizePatchRejectsInvalidChange(t *testing.T) {
	if _, err := BuildOrganizePatch(nil, Change{}); err == nil {
		t.Error("BuildOrganizePatch(empty change) = nil error, want an error")
	}
}
