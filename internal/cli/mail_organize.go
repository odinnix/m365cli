package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/MakersLab-ai/m365cli/internal/mail"
	"github.com/MakersLab-ai/m365cli/internal/output"
)

func newMailFoldersCmd() *cobra.Command {
	var mailbox string
	cmd := &cobra.Command{
		Use:   "folders",
		Short: "List mail folders, nested ones included, with their paths",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			data, err := client.Mail().Folders(cmd.Context(), mbx)
			if err != nil {
				return err
			}
			return emitData(data)
		},
	}
	cmd.Flags().StringVar(&mailbox, "mailbox", "", "mailbox to operate on (defaults to default_mailbox)")
	return cmd
}

func newMailCategoriesCmd() *cobra.Command {
	var mailbox string
	cmd := &cobra.Command{
		Use:   "categories",
		Short: "List the mailbox's Outlook categories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			data, err := client.Mail().MasterCategories(cmd.Context(), mbx)
			if err != nil {
				return err
			}
			return emitData(data)
		},
	}
	cmd.Flags().StringVar(&mailbox, "mailbox", "", "mailbox to operate on (defaults to default_mailbox)")
	return cmd
}

func newMailMoveCmd() *cobra.Command {
	var mailbox, folder string
	cmd := &cobra.Command{
		Use:   "move <message-id>... --folder <folder>",
		Short: "Move messages to a folder (well-known name, path like Inbox/Clients, unique name, or folder id)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if folder == "" {
				return fmt.Errorf("--folder is required")
			}
			_, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			return forEachMessage(args, func(id string) ([]byte, error) {
				return client.Mail().Move(cmd.Context(), mbx, id, folder)
			})
		},
	}
	cmd.Flags().StringVar(&mailbox, "mailbox", "", "mailbox to operate on (defaults to default_mailbox)")
	cmd.Flags().StringVar(&folder, "folder", "", "destination: inbox, archive, deleteditems, junkemail, drafts, sentitems, a path, a unique folder name, or a folder id")
	return cmd
}

func newMailCategorizeCmd() *cobra.Command {
	var mailbox string
	var change mail.Change
	cmd := &cobra.Command{
		Use:   "categorize <message-id>... [--add <category>]... [--remove <category>]... [--clear]",
		Short: "Add, remove or clear Outlook categories on messages",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !change.TouchesCategories() {
				return fmt.Errorf("give --add, --remove or --clear")
			}
			if err := change.Validate(); err != nil {
				return err
			}
			_, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			return forEachMessage(args, func(id string) ([]byte, error) {
				return client.Mail().Organize(cmd.Context(), mbx, id, change)
			})
		},
	}
	cmd.Flags().StringVar(&mailbox, "mailbox", "", "mailbox to operate on (defaults to default_mailbox)")
	cmd.Flags().StringArrayVar(&change.AddCategories, "add", nil, "category to add (repeatable)")
	cmd.Flags().StringArrayVar(&change.RemoveCategories, "remove", nil, "category to remove (repeatable)")
	cmd.Flags().BoolVar(&change.ClearCategories, "clear", false, "remove every category first (with --add, sets the list exactly)")
	return cmd
}

// markFlags are the `mail mark` switches; markChange turns them into a mail.Change.
type markFlags struct{ read, unread, flag, unflag, complete bool }

func markChange(f markFlags) mail.Change {
	var c mail.Change
	if f.read || f.unread {
		read := f.read
		c.Read = &read
	}
	switch {
	case f.flag:
		c.Flag = mail.FlagFlagged
	case f.complete:
		c.Flag = mail.FlagComplete
	case f.unflag:
		c.Flag = mail.FlagNone
	}
	return c
}

func newMailMarkCmd() *cobra.Command {
	var mailbox string
	var f markFlags
	cmd := &cobra.Command{
		Use:   "mark <message-id>... [--read|--unread] [--flag|--complete|--unflag]",
		Short: "Mark messages read or unread and set their follow-up flag",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			change := markChange(f)
			if err := change.Validate(); err != nil {
				return err
			}
			_, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			return forEachMessage(args, func(id string) ([]byte, error) {
				return client.Mail().Organize(cmd.Context(), mbx, id, change)
			})
		},
	}
	cmd.Flags().StringVar(&mailbox, "mailbox", "", "mailbox to operate on (defaults to default_mailbox)")
	cmd.Flags().BoolVar(&f.read, "read", false, "mark as read")
	cmd.Flags().BoolVar(&f.unread, "unread", false, "mark as unread")
	cmd.Flags().BoolVar(&f.flag, "flag", false, "flag for follow-up")
	cmd.Flags().BoolVar(&f.complete, "complete", false, "mark the follow-up flag complete")
	cmd.Flags().BoolVar(&f.unflag, "unflag", false, "clear the follow-up flag")
	cmd.MarkFlagsMutuallyExclusive("read", "unread")
	cmd.MarkFlagsMutuallyExclusive("flag", "complete", "unflag")
	return cmd
}

// forEachMessage applies op to every message id and emits one result per id,
// so a bulk run reports exactly which messages changed. Any failure makes the
// command exit non-zero after the results are written.
func forEachMessage(ids []string, op func(id string) ([]byte, error)) error {
	results := make([]map[string]any, 0, len(ids))
	failed := 0
	for _, id := range ids {
		data, err := op(id)
		if err != nil {
			failed++
			results = append(results, map[string]any{"id": id, "ok": false, "error": err.Error()})
			continue
		}
		results = append(results, map[string]any{"id": id, "ok": true, "result": json.RawMessage(data)})
	}
	if err := output.WriteJSON(os.Stdout, results); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d messages failed", failed, len(ids))
	}
	return nil
}
