package cli

import (
	"testing"

	"github.com/MakersLab-ai/m365cli/internal/mail"
)

func TestMarkChangeMapsFlags(t *testing.T) {
	c := markChange(markFlags{read: true, complete: true})
	if c.Read == nil || !*c.Read || c.Flag != mail.FlagComplete {
		t.Errorf("markChange(read, complete) = %+v, want Read=true Flag=complete", c)
	}
	c = markChange(markFlags{unread: true, unflag: true})
	if c.Read == nil || *c.Read || c.Flag != mail.FlagNone {
		t.Errorf("markChange(unread, unflag) = %+v, want Read=false Flag=notFlagged", c)
	}
	c = markChange(markFlags{flag: true})
	if c.Read != nil || c.Flag != mail.FlagFlagged {
		t.Errorf("markChange(flag) = %+v, want Read unset Flag=flagged", c)
	}
}

func TestMarkChangeWithNoFlagsIsInvalid(t *testing.T) {
	if err := markChange(markFlags{}).Validate(); err == nil {
		t.Error("markChange(no flags).Validate() = nil, want an error")
	}
}
