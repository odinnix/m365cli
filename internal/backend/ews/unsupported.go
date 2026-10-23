package ewsbackend

import (
	"context"

	"github.com/MakersLab-ai/m365cli/internal/backend"
	"github.com/MakersLab-ai/m365cli/internal/contacts"
	"github.com/MakersLab-ai/m365cli/internal/mail"
)

// The contacts, drive and site domains are not implemented for EWS. Contacts is
// out of the mail+calendar scope; drive/SharePoint are cloud-only surfaces that
// do not exist on an on-premise Exchange server. Each method returns
// backend.ErrUnsupported so the CLI fails with a clear message.

type contactSvc struct{}

func (contactSvc) List(context.Context, string, backend.ListOpts) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (contactSvc) Get(context.Context, string, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (contactSvc) Add(context.Context, string, contacts.Contact) ([]byte, error) {
	return nil, backend.ErrUnsupported
}

type driveSvc struct{}

func (driveSvc) List(context.Context, string, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (driveSvc) Search(context.Context, string, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (driveSvc) Get(context.Context, string, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (driveSvc) Download(context.Context, string, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (driveSvc) Upload(context.Context, string, string, []byte) ([]byte, error) {
	return nil, backend.ErrUnsupported
}

type siteSvc struct{}

func (siteSvc) Search(context.Context, string) ([]byte, error) { return nil, backend.ErrUnsupported }
func (siteSvc) ListDrives(context.Context, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (siteSvc) Items(context.Context, string, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (siteSvc) Download(context.Context, string, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}

// Folder listing, categories, moving and organizing messages are implemented
// for Graph only so far; on EWS they report backend.ErrUnsupported.

func (mailSvc) Folders(context.Context, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (mailSvc) MasterCategories(context.Context, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (mailSvc) Move(context.Context, string, string, string) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
func (mailSvc) Organize(context.Context, string, string, mail.Change) ([]byte, error) {
	return nil, backend.ErrUnsupported
}
