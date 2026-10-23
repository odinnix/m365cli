package graphbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/MakersLab-ai/m365cli/internal/mail"
)

// folderSelect bounds one folder level. 250 is Graph's page maximum; a level
// with more folders is reported rather than silently truncated.
const folderSelect = "?$top=250&$select=id,displayName,parentFolderId,childFolderCount,totalItemCount,unreadItemCount"

// maxFolderDepth stops a pathological (or cyclic) folder tree.
const maxFolderDepth = 16

// wellKnownFolders are the Graph well-known folder names Move passes through
// as-is. Recoverable-items and other system folders are deliberately absent.
var wellKnownFolders = map[string]string{
	"inbox": "inbox", "archive": "archive", "deleteditems": "deleteditems", "deleted items": "deleteditems",
	"junkemail": "junkemail", "junk": "junkemail", "junk email": "junkemail",
	"drafts": "drafts", "sentitems": "sentitems", "sent items": "sentitems",
}

// folder is one entry of `m365 mail folders`.
type folder struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	Path            string `json:"path"`
	ParentFolderID  string `json:"parentFolderId,omitempty"`
	TotalItemCount  int    `json:"totalItemCount"`
	UnreadItemCount int    `json:"unreadItemCount"`
}

func (m mailSvc) Folders(ctx context.Context, mailbox string) ([]byte, error) {
	folders, err := m.folderTree(ctx, mailbox)
	if err != nil {
		return nil, err
	}
	return json.Marshal(folders)
}

// folderTree walks the folder hierarchy breadth-first, one request per folder
// that has children, and records each folder's path from the top.
func (m mailSvc) folderTree(ctx context.Context, mailbox string) ([]folder, error) {
	var out []folder
	type level struct {
		suffix, prefix string
		depth          int
	}
	queue := []level{{suffix: "mailFolders" + folderSelect}}
	for len(queue) > 0 {
		lv := queue[0]
		queue = queue[1:]
		body, err := m.c.GetForMailbox(ctx, mailbox, lv.suffix)
		if err != nil {
			return nil, err
		}
		var page struct {
			Value []struct {
				folder
				ChildFolderCount int `json:"childFolderCount"`
			} `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("parse mail folders: %w", err)
		}
		if page.NextLink != "" {
			return nil, fmt.Errorf("a folder level has more than 250 folders; listing it is not supported")
		}
		for _, entry := range page.Value {
			f := entry.folder
			f.Path = f.DisplayName
			if lv.prefix != "" {
				f.Path = lv.prefix + "/" + f.DisplayName
			}
			out = append(out, f)
			if entry.ChildFolderCount > 0 && lv.depth < maxFolderDepth {
				queue = append(queue, level{
					suffix: "mailFolders/" + url.PathEscape(f.ID) + "/childFolders" + folderSelect,
					prefix: f.Path,
					depth:  lv.depth + 1,
				})
			}
		}
	}
	return out, nil
}

func (m mailSvc) MasterCategories(ctx context.Context, mailbox string) ([]byte, error) {
	return unwrapValue(m.c.GetForMailbox(ctx, mailbox, "outlook/masterCategories"))
}

func (m mailSvc) Move(ctx context.Context, mailbox, id, dest string) ([]byte, error) {
	destID, err := m.resolveFolder(ctx, mailbox, dest)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]string{"destinationId": destID})
	if err != nil {
		return nil, err
	}
	body, err := m.c.PostForMailbox(ctx, mailbox, "messages/"+url.PathEscape(id)+"/move", payload)
	if err != nil {
		return nil, err
	}
	var moved struct {
		ID             string `json:"id"`
		ParentFolderID string `json:"parentFolderId"`
	}
	if err := json.Unmarshal(body, &moved); err != nil {
		return nil, fmt.Errorf("parse move response: %w", err)
	}
	return json.Marshal(map[string]string{"id": moved.ID, "previousId": id, "parentFolderId": moved.ParentFolderID})
}

// resolveFolder maps a well-known name, a folder path or unique display name,
// or a folder id to the destinationId Graph's move expects. Ambiguous names
// are refused with every matching path so the caller can be precise.
func (m mailSvc) resolveFolder(ctx context.Context, mailbox, dest string) (string, error) {
	want := strings.Trim(strings.TrimSpace(dest), "/")
	if want == "" {
		return "", fmt.Errorf("no destination folder given")
	}
	if known, ok := wellKnownFolders[strings.ToLower(want)]; ok {
		return known, nil
	}
	folders, err := m.folderTree(ctx, mailbox)
	if err != nil {
		return "", err
	}
	var byPath, byName []folder
	for _, f := range folders {
		if f.ID == want {
			return f.ID, nil
		}
		if strings.EqualFold(f.Path, want) {
			byPath = append(byPath, f)
		}
		if strings.EqualFold(f.DisplayName, want) {
			byName = append(byName, f)
		}
	}
	for _, matches := range [][]folder{byPath, byName} {
		switch len(matches) {
		case 0:
			continue
		case 1:
			return matches[0].ID, nil
		default:
			paths := make([]string, len(matches))
			for i, f := range matches {
				paths[i] = f.Path
			}
			return "", fmt.Errorf("folder %q is ambiguous; use a full path: %s", dest, strings.Join(paths, ", "))
		}
	}
	return "", fmt.Errorf("folder %q not found (see `m365 mail folders`)", dest)
}

func (m mailSvc) Organize(ctx context.Context, mailbox, id string, change mail.Change) ([]byte, error) {
	if err := change.Validate(); err != nil {
		return nil, err
	}
	suffix := "messages/" + url.PathEscape(id)
	var current []string
	if change.TouchesCategories() {
		body, err := m.c.GetForMailbox(ctx, mailbox, suffix+"?$select=categories")
		if err != nil {
			return nil, err
		}
		var msg struct {
			Categories []string `json:"categories"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			return nil, fmt.Errorf("parse message categories: %w", err)
		}
		current = msg.Categories
	}
	payload, err := mail.BuildOrganizePatch(current, change)
	if err != nil {
		return nil, err
	}
	body, err := m.c.PatchForMailbox(ctx, mailbox, suffix, payload)
	if err != nil {
		return nil, err
	}
	// The PATCH response is the whole message; return only what was organized.
	var updated struct {
		ID         string          `json:"id"`
		Categories []string        `json:"categories"`
		IsRead     bool            `json:"isRead"`
		Flag       json.RawMessage `json:"flag,omitempty"`
	}
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, fmt.Errorf("parse updated message: %w", err)
	}
	if updated.Categories == nil {
		updated.Categories = []string{}
	}
	return json.Marshal(updated)
}
