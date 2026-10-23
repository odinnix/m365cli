package mail

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Follow-up flag states a message can be set to (Graph followupFlag.flagStatus).
const (
	FlagNone     = "notFlagged"
	FlagFlagged  = "flagged"
	FlagComplete = "complete"
)

// Change organizes an existing message without touching its content: its
// categories, read state and follow-up flag. Zero values leave a field alone.
type Change struct {
	AddCategories    []string
	RemoveCategories []string
	// ClearCategories drops every existing category before AddCategories apply,
	// so clear + add sets the list exactly.
	ClearCategories bool
	// Read sets the read state when non-nil.
	Read *bool
	// Flag sets the follow-up flag when non-empty: FlagNone, FlagFlagged or FlagComplete.
	Flag string
}

// TouchesCategories reports whether applying c needs the message's current categories.
func (c Change) TouchesCategories() bool {
	return c.ClearCategories || len(c.AddCategories) > 0 || len(c.RemoveCategories) > 0
}

// Validate rejects a change that does nothing, an unknown flag state, or a
// category that is both added and removed.
func (c Change) Validate() error {
	if !c.TouchesCategories() && c.Read == nil && c.Flag == "" {
		return errors.New("nothing to change: give categories, a read state or a flag")
	}
	switch c.Flag {
	case "", FlagNone, FlagFlagged, FlagComplete:
	default:
		return fmt.Errorf("unknown flag status %q (want %s, %s or %s)", c.Flag, FlagFlagged, FlagComplete, FlagNone)
	}
	for _, add := range c.AddCategories {
		for _, remove := range c.RemoveCategories {
			if strings.EqualFold(add, remove) {
				return fmt.Errorf("category %q is both added and removed", add)
			}
		}
	}
	return nil
}

// MergeCategories applies c's category changes to current. Outlook compares
// category names case-insensitively, so matching and de-duplication do too; the
// existing order and spelling are kept and additions are appended. The result
// is never nil, because Graph needs an explicit [] to clear the list.
func MergeCategories(current []string, c Change) []string {
	out := []string{}
	has := func(name string) bool {
		for _, existing := range out {
			if strings.EqualFold(existing, name) {
				return true
			}
		}
		return false
	}
	removed := func(name string) bool {
		for _, r := range c.RemoveCategories {
			if strings.EqualFold(r, name) {
				return true
			}
		}
		return false
	}
	if !c.ClearCategories {
		for _, name := range current {
			if !removed(name) && !has(name) {
				out = append(out, name)
			}
		}
	}
	for _, name := range c.AddCategories {
		if name = strings.TrimSpace(name); name != "" && !has(name) {
			out = append(out, name)
		}
	}
	return out
}

// BuildOrganizePatch builds the Graph PATCH body for a message: only the fields
// c changes, with categories merged against the message's current ones.
func BuildOrganizePatch(current []string, c Change) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	patch := map[string]any{}
	if c.TouchesCategories() {
		patch["categories"] = MergeCategories(current, c) // an empty list is how Graph clears categories
	}
	if c.Read != nil {
		patch["isRead"] = *c.Read
	}
	if c.Flag != "" {
		patch["flag"] = map[string]string{"flagStatus": c.Flag}
	}
	return json.Marshal(patch) // map keys encode sorted, so the payload is deterministic
}
