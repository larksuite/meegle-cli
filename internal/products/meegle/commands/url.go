// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewURLCmd returns the `meegle url` command group. It is a pure local
// utility — no network, no auth — so it is always available regardless of
// login state.
func NewURLCmd() *cobra.Command {
	urlCmd := &cobra.Command{
		Use:   "url",
		Short: "Parse Meegle / Feishu Project URLs into structured fields",
		Long: `Parse Meegle / Feishu Project URLs into structured fields.

Offline, no-network utility. Use from skills when the user pastes a URL —
branch on the returned url_kind to decide whether the link points to a
work item, a view, a chart, a setting page, etc., instead of guessing from
the raw path.`,
	}
	urlCmd.AddCommand(newURLDecodeCmd())
	return urlCmd
}

func newURLDecodeCmd() *cobra.Command {
	var (
		rawURL string
		format string
	)
	cmd := &cobra.Command{
		Use:   "decode",
		Short: "Decode a Meegle / Feishu Project URL into structured fields",
		Long: `Decode a Meegle / Feishu Project URL into structured fields.

Output is a JSON object whose "url_kind" field identifies the page type
(workitem_detail, view_story, chart_detail, setting_permission, …). For
unrecognised URLs "url_kind" is "unknown" — callers should refuse rather
than guess.

Examples:
  meegle url decode --url https://project.feishu.cn/xopenapp/story/detail/7092625364
  meegle url decode --url /xopenapp/storyView/9988 --format table`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if rawURL == "" {
				return fmt.Errorf("--url is required")
			}
			res, err := DecodeURL(rawURL)
			if err != nil {
				return err
			}
			text, err := renderPayload(res, format)
			if err != nil {
				return err
			}
			fmt.Println(text)
			return nil
		},
	}
	cmd.Flags().StringVar(&rawURL, "url", "", "URL to decode (required)")
	cmd.Flags().StringVar(&format, "format", "json", "Output format: json, table, ndjson")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}
