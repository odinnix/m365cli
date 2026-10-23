package graphbackend_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MakersLab-ai/m365cli/internal/backend"
	graphbackend "github.com/MakersLab-ai/m365cli/internal/backend/graph"
	"github.com/MakersLab-ai/m365cli/internal/config"
	"github.com/MakersLab-ai/m365cli/internal/graph"
	"github.com/MakersLab-ai/m365cli/internal/mail"
)

const folderSelect = "$top=250&$select=id,displayName,parentFolderId,childFolderCount,totalItemCount,unreadItemCount"

// call is one request the routed fake Graph received.
type call struct{ method, uri, body string }

// newRoutedBackend serves a canned response per "METHOD uri" and records every
// call, for flows that make several different requests (list, then move).
func newRoutedBackend(t *testing.T, cfg *config.Config, routes map[string]string, calls *[]call) backend.Backend {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.RequestURI()
		*calls = append(*calls, call{r.Method, r.URL.RequestURI(), string(body)})
		resp, ok := routes[key]
		if !ok {
			t.Errorf("unexpected request %s", key)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	c := graph.New(cfg, fakeToken{})
	c.BaseURL = srv.URL
	return graphbackend.New(c)
}

// A root with Inbox (one child, "Clients", which has "Acme") and Archive.
func folderRoutes() map[string]string {
	return map[string]string{
		"GET /users/" + mbx + "/mailFolders?" + folderSelect: `{"value":[
			{"id":"INBOX","displayName":"Inbox","parentFolderId":"ROOT","childFolderCount":1,"totalItemCount":10,"unreadItemCount":2},
			{"id":"ARCH","displayName":"Archive","parentFolderId":"ROOT","childFolderCount":0,"totalItemCount":5,"unreadItemCount":0}]}`,
		"GET /users/" + mbx + "/mailFolders/INBOX/childFolders?" + folderSelect: `{"value":[
			{"id":"CLIENTS","displayName":"Clients","parentFolderId":"INBOX","childFolderCount":1,"totalItemCount":3,"unreadItemCount":1}]}`,
		"GET /users/" + mbx + "/mailFolders/CLIENTS/childFolders?" + folderSelect: `{"value":[
			{"id":"ACME","displayName":"Acme","parentFolderId":"CLIENTS","childFolderCount":0,"totalItemCount":1,"unreadItemCount":0}]}`,
	}
}

func TestMailFoldersWalksChildFoldersWithPaths(t *testing.T) {
	var calls []call
	be := newRoutedBackend(t, mailboxCfg(), folderRoutes(), &calls)
	raw, err := be.Mail().Folders(context.Background(), mbx)
	if err != nil {
		t.Fatal(err)
	}
	var got []struct {
		ID, DisplayName, Path, ParentFolderID string
		TotalItemCount, UnreadItemCount       int
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	paths := map[string]string{}
	for _, f := range got {
		paths[f.ID] = f.Path
	}
	want := map[string]string{"INBOX": "Inbox", "CLIENTS": "Inbox/Clients", "ACME": "Inbox/Clients/Acme", "ARCH": "Archive"}
	for id, path := range want {
		if paths[id] != path {
			t.Errorf("folder %s path = %q, want %q (all: %v)", id, paths[id], path, paths)
		}
	}
	if len(got) != 4 {
		t.Errorf("got %d folders, want 4", len(got))
	}
}

func TestMailFoldersRefusesOutOfScopeMailboxBeforeIO(t *testing.T) {
	var calls []call
	be := newRoutedBackend(t, mailboxCfg(), nil, &calls)
	if _, err := be.Mail().Folders(context.Background(), "someone.else@example.com"); err == nil {
		t.Error("Folders(out-of-scope mailbox) = nil error, want refusal")
	}
	if len(calls) != 0 {
		t.Errorf("made %d requests for an out-of-scope mailbox, want 0", len(calls))
	}
}

func TestMailMasterCategoriesUnwrapsValue(t *testing.T) {
	var calls []call
	routes := map[string]string{
		"GET /users/" + mbx + "/outlook/masterCategories": `{"value":[{"id":"1","displayName":"Clients","color":"preset0"}]}`,
	}
	be := newRoutedBackend(t, mailboxCfg(), routes, &calls)
	raw, err := be.Mail().MasterCategories(context.Background(), mbx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "[") || !strings.Contains(string(raw), "Clients") {
		t.Errorf("MasterCategories = %s, want the unwrapped value array", raw)
	}
}

func TestMailMoveToWellKnownFolderSkipsLookup(t *testing.T) {
	var calls []call
	routes := map[string]string{
		"POST /users/" + mbx + "/messages/MSG1/move": `{"id":"MSG1-NEW","parentFolderId":"ARCH","subject":"Hi"}`,
	}
	be := newRoutedBackend(t, mailboxCfg(), routes, &calls)
	raw, err := be.Mail().Move(context.Background(), mbx, "MSG1", "Archive")
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].body != `{"destinationId":"archive"}` {
		t.Errorf("calls = %+v, want one move with destinationId archive", calls)
	}
	if !strings.Contains(string(raw), `"MSG1-NEW"`) || !strings.Contains(string(raw), `"ARCH"`) {
		t.Errorf("Move result = %s, want the moved message's new id and folder", raw)
	}
}

func TestMailMoveResolvesFolderPath(t *testing.T) {
	var calls []call
	routes := folderRoutes()
	routes["POST /users/"+mbx+"/messages/MSG1/move"] = `{"id":"MSG1-NEW","parentFolderId":"ACME"}`
	be := newRoutedBackend(t, mailboxCfg(), routes, &calls)
	if _, err := be.Mail().Move(context.Background(), mbx, "MSG1", "inbox/clients/acme"); err != nil {
		t.Fatal(err)
	}
	last := calls[len(calls)-1]
	if last.method != http.MethodPost || last.body != `{"destinationId":"ACME"}` {
		t.Errorf("last call = %+v, want a move to ACME", last)
	}
}

func TestMailMoveResolvesUniqueDisplayName(t *testing.T) {
	var calls []call
	routes := folderRoutes()
	routes["POST /users/"+mbx+"/messages/MSG1/move"] = `{"id":"MSG1-NEW","parentFolderId":"CLIENTS"}`
	be := newRoutedBackend(t, mailboxCfg(), routes, &calls)
	if _, err := be.Mail().Move(context.Background(), mbx, "MSG1", "Clients"); err != nil {
		t.Fatal(err)
	}
	if last := calls[len(calls)-1]; last.body != `{"destinationId":"CLIENTS"}` {
		t.Errorf("move body = %s, want destinationId CLIENTS", last.body)
	}
}

func TestMailMoveRefusesAmbiguousName(t *testing.T) {
	var calls []call
	routes := folderRoutes()
	// A second "Acme", under Archive.
	routes["GET /users/"+mbx+"/mailFolders?"+folderSelect] = `{"value":[
		{"id":"INBOX","displayName":"Inbox","childFolderCount":1},
		{"id":"ARCH","displayName":"Archive","childFolderCount":1}]}`
	routes["GET /users/"+mbx+"/mailFolders/ARCH/childFolders?"+folderSelect] = `{"value":[{"id":"ACME2","displayName":"Acme","childFolderCount":0}]}`
	routes["GET /users/"+mbx+"/mailFolders/CLIENTS/childFolders?"+folderSelect] = `{"value":[{"id":"ACME","displayName":"Acme","childFolderCount":0}]}`
	be := newRoutedBackend(t, mailboxCfg(), routes, &calls)
	_, err := be.Mail().Move(context.Background(), mbx, "MSG1", "Acme")
	if err == nil || !strings.Contains(err.Error(), "Inbox/Clients/Acme") || !strings.Contains(err.Error(), "Archive/Acme") {
		t.Errorf("Move(ambiguous) error = %v, want one naming both paths", err)
	}
	for _, c := range calls {
		if c.method == http.MethodPost {
			t.Errorf("moved despite an ambiguous folder name: %+v", c)
		}
	}
}

func TestMailMoveRefusesUnknownFolder(t *testing.T) {
	var calls []call
	be := newRoutedBackend(t, mailboxCfg(), folderRoutes(), &calls)
	if _, err := be.Mail().Move(context.Background(), mbx, "MSG1", "Nowhere"); err == nil {
		t.Error("Move(unknown folder) = nil error, want refusal")
	}
}

func TestMailOrganizeReadsCategoriesThenPatches(t *testing.T) {
	var calls []call
	routes := map[string]string{
		"GET /users/" + mbx + "/messages/MSG1?$select=categories": `{"categories":["Clients"]}`,
		"PATCH /users/" + mbx + "/messages/MSG1":                  `{"id":"MSG1","categories":["Clients","Todo"],"isRead":true,"flag":{"flagStatus":"flagged"},"body":{"content":"secret"}}`,
	}
	be := newRoutedBackend(t, mailboxCfg(), routes, &calls)
	read := true
	raw, err := be.Mail().Organize(context.Background(), mbx, "MSG1", mail.Change{AddCategories: []string{"Todo"}, Read: &read, Flag: mail.FlagFlagged})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1].body != `{"categories":["Clients","Todo"],"flag":{"flagStatus":"flagged"},"isRead":true}` {
		t.Errorf("calls = %+v, want a categories read then one merged PATCH", calls)
	}
	if strings.Contains(string(raw), "secret") {
		t.Errorf("Organize result = %s, want only id/categories/isRead/flag, not the body", raw)
	}
}

func TestMailOrganizeWithoutCategoriesSkipsTheRead(t *testing.T) {
	var calls []call
	routes := map[string]string{
		"PATCH /users/" + mbx + "/messages/MSG1": `{"id":"MSG1","isRead":false}`,
	}
	be := newRoutedBackend(t, mailboxCfg(), routes, &calls)
	unread := false
	if _, err := be.Mail().Organize(context.Background(), mbx, "MSG1", mail.Change{Read: &unread}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].body != `{"isRead":false}` {
		t.Errorf("calls = %+v, want a single PATCH {\"isRead\":false}", calls)
	}
}

func TestMailOrganizeRejectsEmptyChangeBeforeIO(t *testing.T) {
	var calls []call
	be := newRoutedBackend(t, mailboxCfg(), nil, &calls)
	if _, err := be.Mail().Organize(context.Background(), mbx, "MSG1", mail.Change{}); err == nil {
		t.Error("Organize(empty change) = nil error, want refusal")
	}
	if len(calls) != 0 {
		t.Errorf("made %d requests for an empty change, want 0", len(calls))
	}
}
