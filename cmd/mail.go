package cmd

import (
	"github.com/spf13/cobra"
)

var mailCmd = &cobra.Command{
	Use:   "mail",
	Short: "Mail operations",
}

// Proton mailbox label IDs (from WebClients MAILBOX_LABEL_IDS enum).
var mailboxLabelIDs = map[string]string{
	"inbox":   "0",
	"drafts":  "8",
	"sent":    "7",
	"trash":   "3",
	"spam":    "4",
	"archive": "6",
	"starred": "10",
	"all":     "5",
}

var (
	mailTo      string
	mailSubject string
	mailBody    string

	mailListFolder   string
	mailListPage     int
	mailListPageSize int
	mailListUnread   bool

	mailMoveFolder string

	mailMarkRead    bool
	mailMarkUnread  bool
	mailMarkStarred bool
	mailMarkUnstar  bool

	mailSearchKeyword string
	mailSearchFrom    string
	mailSearchTo      string
	mailSearchSubject string
	mailSearchAfter   string
	mailSearchBefore  string
	mailSearchFolder  string
	mailSearchLimit   int
)

func init() {
	// list
	mailListCmd.Flags().StringVar(&mailListFolder, "folder", "inbox", "Folder (inbox, sent, drafts, trash, spam, archive, starred, all)")
	mailListCmd.Flags().IntVar(&mailListPage, "page", 0, "Page number (0-based)")
	mailListCmd.Flags().IntVar(&mailListPageSize, "page-size", 25, "Messages per page")
	mailListCmd.Flags().BoolVar(&mailListUnread, "unread", false, "Show only unread messages")

	// send
	mailSendCmd.Flags().StringVar(&mailTo, "to", "", "Recipient email")
	mailSendCmd.Flags().StringVar(&mailSubject, "subject", "", "Subject")
	mailSendCmd.Flags().StringVar(&mailBody, "body", "", "Message body (plain text)")

	// move
	mailMoveCmd.Flags().StringVar(&mailMoveFolder, "folder", "", "Destination folder (inbox, sent, drafts, trash, spam, archive, starred)")
	_ = mailMoveCmd.MarkFlagRequired("folder")

	// mark
	mailMarkCmd.Flags().BoolVar(&mailMarkRead, "read", false, "Mark as read")
	mailMarkCmd.Flags().BoolVar(&mailMarkUnread, "unread", false, "Mark as unread")
	mailMarkCmd.Flags().BoolVar(&mailMarkStarred, "starred", false, "Add star")
	mailMarkCmd.Flags().BoolVar(&mailMarkUnstar, "unstar", false, "Remove star")

	// search
	mailSearchCmd.Flags().StringVar(&mailSearchKeyword, "keyword", "", "Search keyword")
	mailSearchCmd.Flags().StringVar(&mailSearchFrom, "from", "", "Filter by sender")
	mailSearchCmd.Flags().StringVar(&mailSearchTo, "to", "", "Filter by recipient")
	mailSearchCmd.Flags().StringVar(&mailSearchSubject, "subject", "", "Filter by subject")
	mailSearchCmd.Flags().StringVar(&mailSearchAfter, "after", "", "After date (YYYY-MM-DD)")
	mailSearchCmd.Flags().StringVar(&mailSearchBefore, "before", "", "Before date (YYYY-MM-DD)")
	mailSearchCmd.Flags().StringVar(&mailSearchFolder, "folder", "all", "Folder to search in")
	mailSearchCmd.Flags().IntVar(&mailSearchLimit, "limit", 25, "Max results")

	// attachments
	mailAttachmentsCmd.AddCommand(mailAttachmentsListCmd, mailAttachmentsDownloadCmd)

	// subgroups
	registerMailFilters()
	registerMailLabels()
	registerMailAddresses()

	mailCmd.AddCommand(mailListCmd, mailReadCmd, mailSendCmd, mailTrashCmd, mailDeleteCmd, mailMoveCmd, mailMarkCmd, mailSearchCmd, mailAttachmentsCmd)
	rootCmd.AddCommand(mailCmd)
}
