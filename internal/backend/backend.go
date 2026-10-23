// Package backend defines the transport-agnostic seam between the CLI command
// layer and a concrete mailbox/site provider. The Microsoft Graph backend
// (internal/backend/graph) is the only implementation today; an on-premise
// Exchange / EWS backend is planned to sit alongside it, implementing the SAME
// interfaces so the CLI layer never learns which transport it talks to.
//
// The seam is drawn at the DOMAIN-OPERATION level (list messages, send mail,
// list events, ...), not at the HTTP-verb level: an EWS (SOAP/XML) backend
// cannot implement "GET a Graph REST suffix", but it can implement "list the
// messages in a mailbox".
//
// Read methods return raw JSON documents ([]byte): for the Graph backend this
// is exactly the bytes it emits today, so routing the CLI through this seam is
// provably byte-for-byte unchanged. That JSON shape — the current Graph
// projection of each $select — is the contract a second backend must reproduce.
// Status envelopes (e.g. {"sent":true,...}) stay in the CLI layer; backends
// return only the underlying data (or an id / error).
package backend

import (
	"context"
	"errors"

	"github.com/MakersLab-ai/m365cli/internal/calendar"
	"github.com/MakersLab-ai/m365cli/internal/contacts"
	"github.com/MakersLab-ai/m365cli/internal/mail"
)

// ErrUnsupported is returned by a backend for an operation it does not implement
// (e.g. the EWS backend before a capability is built out). It reads as a clear
// "not supported by this backend".
var ErrUnsupported = errors.New("operation not supported by this backend")

// Backend is the capability aggregate a CLI command resolves once per
// invocation. Every sub-service is mailbox- or site-scoped through the same
// allowlist guardrails the concrete backend enforces internally.
type Backend interface {
	Mail() MailService
	Calendar() CalendarService
	Contacts() ContactService
	Drive() DriveService
	Sites() SiteService
}

// ListOpts bounds a simple listing (messages, contacts).
type ListOpts struct {
	Max int
}

// SearchOpts bounds a full-text search.
type SearchOpts struct {
	Query string
	Max   int
}

// MailService covers every `m365 mail` operation. Read methods return the
// JSON the command renders verbatim; Send/Reply return only an error (the CLI
// owns the send guardrail and the status envelope); CreateDraft/CreateReplyDraft
// return the created draft id. MailboxDelta backs `mail watch` and matches the
// watch package's GraphDelta port exactly.
type MailService interface {
	List(ctx context.Context, mailbox string, opts ListOpts) ([]byte, error)
	Read(ctx context.Context, mailbox, id string) ([]byte, error)
	Search(ctx context.Context, mailbox string, opts SearchOpts) ([]byte, error)

	Send(ctx context.Context, mailbox string, msg mail.Message) error
	CreateDraft(ctx context.Context, mailbox string, msg mail.Message) (id string, err error)

	// ReplyContext returns the addresses a reply/replyAll would reach, so the
	// CLI can apply the send guardrail before sending.
	ReplyContext(ctx context.Context, mailbox, id string, replyAll bool) (recipients []string, err error)
	// Reply and CreateReplyDraft take the body as authored (mail.Body carries
	// text-vs-HTML), not pre-rendered: the right rendering is transport-specific
	// — Graph's reply path is HTML-only, EWS types its bodies explicitly.
	Reply(ctx context.Context, mailbox, id string, body mail.Body, replyAll bool) error
	CreateReplyDraft(ctx context.Context, mailbox, id string, body mail.Body, replyAll bool) (draftID string, err error)

	// AddInlineImage attaches an image to an existing draft and marks it
	// inline, so the body can reference it as <img src="cid:…">. It is what
	// makes a signature block with logos survive into a draft.
	AddInlineImage(ctx context.Context, mailbox, msgID string, img mail.InlineImage) error

	Attachments(ctx context.Context, mailbox, msgID string) ([]byte, error)
	// GetAttachment returns the attachment resource JSON (the CLI decodes the
	// base64 content when --out is given).
	GetAttachment(ctx context.Context, mailbox, msgID, attID string) ([]byte, error)

	// MailboxDelta polls a mailbox delta resource (initial suffix or absolute
	// deltaLink/cursor). Its signature matches watch.GraphDelta so MailService
	// satisfies the watch port directly.
	MailboxDelta(ctx context.Context, mailbox, urlOrCursor string) ([]byte, error)

	// Folders lists the mailbox's mail folders, nested folders included, each
	// with its slash-separated path from the top (e.g. "Inbox/Clients").
	Folders(ctx context.Context, mailbox string) ([]byte, error)
	// MasterCategories lists the mailbox's Outlook categories (name and color).
	MasterCategories(ctx context.Context, mailbox string) ([]byte, error)
	// Move moves a message to a folder given as a well-known name (inbox,
	// archive, deleteditems, junkemail, drafts, sentitems), a folder path or
	// unique display name, or a folder id. It returns the moved message's id and
	// folder: a moved message gets a new id.
	Move(ctx context.Context, mailbox, id, folder string) ([]byte, error)
	// Organize changes a message's categories, read state and follow-up flag,
	// and returns the resulting id, categories, isRead and flag.
	Organize(ctx context.Context, mailbox, id string, change mail.Change) ([]byte, error)
}

// CalListOpts selects either a plain event list or a calendarView time window.
type CalListOpts struct {
	Start string
	End   string
	Max   int
}

// ScheduleQuery parameterizes a free/busy (getSchedule) lookup.
type ScheduleQuery struct {
	Schedules []string
	Start     string
	End       string
	TimeZone  string
	Interval  int
}

// FindTimesQuery parameterizes a findMeetingTimes lookup.
type FindTimesQuery struct {
	Attendees []string
	Start     string
	End       string
	TimeZone  string
	Duration  string
	Max       int
}

// CalendarService covers every `m365 calendar` operation.
type CalendarService interface {
	List(ctx context.Context, mailbox string, opts CalListOpts) ([]byte, error)
	Get(ctx context.Context, mailbox, id string) ([]byte, error)
	Create(ctx context.Context, mailbox string, ev calendar.Event) ([]byte, error)
	Update(ctx context.Context, mailbox, id string, ev calendar.Event) ([]byte, error)
	Delete(ctx context.Context, mailbox, id string) error
	FreeBusy(ctx context.Context, mailbox string, q ScheduleQuery) ([]byte, error)
	FindTimes(ctx context.Context, mailbox string, q FindTimesQuery) ([]byte, error)
}

// ContactService covers every `m365 contacts` operation.
type ContactService interface {
	List(ctx context.Context, mailbox string, opts ListOpts) ([]byte, error)
	Get(ctx context.Context, mailbox, id string) ([]byte, error)
	Add(ctx context.Context, mailbox string, c contacts.Contact) ([]byte, error)
}

// DriveService covers every `m365 drive` operation (OneDrive, user-scoped).
// Download/Upload move raw bytes and never round-trip through JSON.
type DriveService interface {
	List(ctx context.Context, mailbox, path string) ([]byte, error)
	Search(ctx context.Context, mailbox, query string) ([]byte, error)
	Get(ctx context.Context, mailbox, id string) ([]byte, error)
	Download(ctx context.Context, mailbox, id string) ([]byte, error)
	Upload(ctx context.Context, mailbox, destName string, content []byte) ([]byte, error)
}

// SiteService covers every `m365 sp` operation (SharePoint, site-scoped).
// Search is intentionally un-gated discovery (find a site id to allowlist).
type SiteService interface {
	Search(ctx context.Context, query string) ([]byte, error)
	ListDrives(ctx context.Context, siteID string) ([]byte, error)
	Items(ctx context.Context, siteID, path string) ([]byte, error)
	Download(ctx context.Context, siteID, itemID string) ([]byte, error)
}
