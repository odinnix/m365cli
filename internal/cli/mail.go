package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MakersLab-ai/m365cli/internal/backend"
	"github.com/MakersLab-ai/m365cli/internal/config"
	"github.com/MakersLab-ai/m365cli/internal/mail"
	"github.com/MakersLab-ai/m365cli/internal/output"
)

// newMailCmd is the mail domain root (app-only, scoped by allowed_mailboxes).
func newMailCmd() *cobra.Command {
	mail := &cobra.Command{
		Use:   "mail",
		Short: "Mailbox operations (app-only, scoped by allowed_mailboxes)",
	}
	mail.AddCommand(
		newMailListCmd(), newMailReadCmd(), newMailSearchCmd(),
		newMailSendCmd(), newMailDraftCmd(), newMailReplyCmd(),
		newMailAttachmentsCmd(), newMailGetAttachmentCmd(), newMailWatchCmd(),
		newMailFoldersCmd(), newMailCategoriesCmd(), newMailMoveCmd(),
		newMailCategorizeCmd(), newMailMarkCmd(),
	)
	return mail
}

// mailContext loads config, resolves the mailbox (--mailbox or default_mailbox),
// and builds the backend. The allowlist is enforced again inside the backend.
func mailContext(mailbox string) (*config.Config, backend.Backend, string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, "", err
	}
	mbx := mailbox
	if mbx == "" {
		mbx = cfg.DefaultMailbox
	}
	if mbx == "" {
		return nil, nil, "", fmt.Errorf("no mailbox given (use --mailbox or set default_mailbox)")
	}
	be, err := newBackend(cfg)
	if err != nil {
		return nil, nil, "", err
	}
	return cfg, be, mbx, nil
}

// emitData renders a backend JSON document as the stable stdout envelope.
func emitData(body []byte) error {
	return output.WriteJSON(os.Stdout, json.RawMessage(body))
}

func newMailListCmd() *cobra.Command {
	var mailbox string
	var max int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List messages in a mailbox",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			data, err := client.Mail().List(cmd.Context(), mbx, backend.ListOpts{Max: max})
			if err != nil {
				return err
			}
			return emitData(data)
		},
	}
	cmd.Flags().StringVar(&mailbox, "mailbox", "", "mailbox to operate on (defaults to default_mailbox)")
	cmd.Flags().IntVar(&max, "max", 25, "maximum number of messages to return")
	return cmd
}

func newMailReadCmd() *cobra.Command {
	var mailbox string
	cmd := &cobra.Command{
		Use:   "read <message-id>",
		Short: "Read a single message (with body)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			data, err := client.Mail().Read(cmd.Context(), mbx, args[0])
			if err != nil {
				return err
			}
			return emitData(data)
		},
	}
	cmd.Flags().StringVar(&mailbox, "mailbox", "", "mailbox to operate on (defaults to default_mailbox)")
	return cmd
}

func newMailSearchCmd() *cobra.Command {
	var mailbox string
	var max int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search messages in a mailbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			data, err := client.Mail().Search(cmd.Context(), mbx, backend.SearchOpts{Query: args[0], Max: max})
			if err != nil {
				return err
			}
			return emitData(data)
		},
	}
	cmd.Flags().StringVar(&mailbox, "mailbox", "", "mailbox to operate on (defaults to default_mailbox)")
	cmd.Flags().IntVar(&max, "max", 25, "maximum number of results")
	return cmd
}

func newMailSendCmd() *cobra.Command {
	var mailbox, subject, bodyFile, cc string
	var to []string
	var asHTML bool
	cmd := &cobra.Command{
		Use:   "send --to <addr> --subject <s> --body-file <f>",
		Short: "Send a message — falls back to a draft if any recipient is outside send_allow",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			msg, err := composeMessage(subject, bodyFile, to, cc, asHTML)
			if err != nil {
				return err
			}

			recipients := append(append([]string{}, msg.To...), msg.Cc...)
			plan := mail.PlanSend(cfg, recipients)
			if plan.Action == mail.DraftOnly {
				fmt.Fprintf(os.Stderr, "send guardrail: %v not in send_allow — saving as draft for review\n", plan.Blocked)
				return createDraft(cmd.Context(), client, mbx, msg, plan.Blocked, nil)
			}

			if err := client.Mail().Send(cmd.Context(), mbx, msg); err != nil {
				return err
			}
			return output.WriteJSON(os.Stdout, map[string]any{"sent": true, "mailbox": mbx, "to": msg.To})
		},
	}
	addComposeFlags(cmd, &mailbox, &subject, &bodyFile, &cc, &to, &asHTML)
	return cmd
}

func newMailDraftCmd() *cobra.Command {
	var mailbox, subject, bodyFile, cc string
	var to, inlineImages []string
	var asHTML bool
	cmd := &cobra.Command{
		Use:   "draft --to <addr> --subject <s> --body-file <f>",
		Short: "Create a draft message (never sends)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, client, mbx, err := mailContext(mailbox)
			if err != nil {
				return err
			}
			msg, err := composeMessage(subject, bodyFile, to, cc, asHTML)
			if err != nil {
				return err
			}
			images, err := loadInlineImages(inlineImages)
			if err != nil {
				return err
			}
			return createDraft(cmd.Context(), client, mbx, msg, nil, images)
		},
	}
	addComposeFlags(cmd, &mailbox, &subject, &bodyFile, &cc, &to, &asHTML)
	cmd.Flags().StringArrayVar(&inlineImages, "inline-image", nil, "attach an inline image as cid=path, referenced from the body as <img src=\"cid:…\"> (repeatable)")
	return cmd
}

// --- shared compose helpers ---

func addComposeFlags(cmd *cobra.Command, mailbox, subject, bodyFile, cc *string, to *[]string, asHTML *bool) {
	cmd.Flags().StringVar(mailbox, "mailbox", "", "mailbox to send from (defaults to default_mailbox)")
	cmd.Flags().StringSliceVar(to, "to", nil, "recipient address (repeatable)")
	cmd.Flags().StringVar(cc, "cc", "", "cc address (comma-separated)")
	cmd.Flags().StringVar(subject, "subject", "", "message subject")
	cmd.Flags().StringVar(bodyFile, "body-file", "", "path to a file containing the message body (avoids shell escaping)")
	cmd.Flags().BoolVar(asHTML, "html", false, "the body file already contains HTML — send it as an HTML body instead of plain text")
}

// noteAutoHTML tells the caller on stderr that the body was recognised as HTML
// without --html. Sending it as text/plain would have shown the raw tags to the
// recipient, so the CLI corrects it — but silently changing the content type is
// the kind of thing an author should hear about.
func noteAutoHTML(body string, asHTML bool) {
	if !asHTML && mail.LooksLikeHTML(body) {
		fmt.Fprintln(os.Stderr, "note: body file starts with HTML — sending as an HTML body (pass --html to state this explicitly)")
	}
}

func composeMessage(subject, bodyFile string, to []string, cc string, asHTML bool) (mail.Message, error) {
	if len(to) == 0 {
		return mail.Message{}, fmt.Errorf("at least one --to recipient is required")
	}
	if bodyFile == "" {
		return mail.Message{}, fmt.Errorf("--body-file is required (use a file to avoid shell escaping)")
	}
	body, err := os.ReadFile(bodyFile)
	if err != nil {
		return mail.Message{}, fmt.Errorf("read --body-file: %w", err)
	}
	var ccList []string
	if cc != "" {
		ccList = splitComma(cc)
	}
	noteAutoHTML(string(body), asHTML)
	return mail.Message{
		Subject: subject,
		Body:    mail.Body{Content: string(body), HTML: asHTML},
		To:      to,
		Cc:      ccList,
	}, nil
}

func createDraft(ctx context.Context, client backend.Backend, mbx string, msg mail.Message, blocked []string, images []mail.InlineImage) error {
	id, err := client.Mail().CreateDraft(ctx, mbx, msg)
	if err != nil {
		return err
	}
	if err := attachInlineImages(ctx, client, mbx, id, images); err != nil {
		return err
	}
	return output.WriteJSON(os.Stdout, map[string]any{
		"sent":         false,
		"draft":        true,
		"draft_id":     id,
		"mailbox":      mbx,
		"blocked":      blocked,
		"draftReason":  draftReason(blocked),
		"inlineImages": len(images),
	})
}

func draftReason(blocked []string) string {
	if len(blocked) == 0 {
		return "draft requested"
	}
	return "recipients outside send_allow"
}

func splitComma(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
