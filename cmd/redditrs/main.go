package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jhartum/redditrs/internal/reddit"
	"github.com/spf13/cobra"
)

var (
	formatFlag string
	fullFlag   bool
	fieldsFlag string
	version    = "dev"
)

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout))
}

func runCLI(args []string, stdout io.Writer) int {
	root := newRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	if command, err := root.ExecuteC(); err != nil {
		if structured, ok := err.(*cliError); ok {
			_, _ = fmt.Fprint(stdout, renderCLIError(structured))
			return structured.exitCode()
		}
		structured := usageError(err, command.Name())
		_, _ = fmt.Fprint(stdout, renderCLIError(structured))
		return structured.exitCode()
	}
	return 0
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "redditrs",
		Short:         "Reddit research for agents",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if formatFlag != "toon" && formatFlag != "json" {
				return &cliError{Message: "--format must be toon or json", Code: "VALIDATION_ERROR"}
			}
			return nil
		},
		RunE: runHome,
	}
	root.PersistentFlags().StringVar(&formatFlag, "format", "toon", "output format: toon or json")
	root.PersistentFlags().StringVar(&fieldsFlag, "fields", "", "comma-separated output fields")
	root.PersistentFlags().BoolVar(&fullFlag, "full", false, "show complete text fields")
	root.AddCommand(newURLExtractCommand())
	root.AddCommand(newStatusCommand())
	root.AddCommand(newSearchCommand())
	root.AddCommand(newThreadCommand())
	root.AddCommand(newSubredditsCommand())
	root.AddCommand(newTrendsCommand())
	root.AddCommand(newResolveSubredditsCommand())
	root.AddCommand(newPackCommand())
	return root
}

func newURLExtractCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "url-extract <url_or_id>",
		Short:   "Normalize a Reddit URL or ID",
		Example: "  redditrs url-extract https://www.reddit.com/r/ClaudeCode/comments/1abc234/",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(cmd.OutOrStdout(), renderURLReference(reddit.ExtractURL(args[0])))
			return err
		},
	}
}

func renderURLReference(ref reddit.URLReference) string {
	if formatFlag == "json" {
		encoded, _ := json.Marshal(ref)
		return string(encoded)
	}

	lines := []string{"kind: " + ref.Kind}
	if ref.Subreddit != "" {
		lines = append(lines, "subreddit: "+toonString(ref.Subreddit))
	}
	if ref.PostID != "" {
		lines = append(lines, "post_id: "+toonString(ref.PostID))
	}
	if ref.CommentID != "" {
		lines = append(lines, "comment_id: "+toonString(ref.CommentID))
	}
	if ref.Username != "" {
		lines = append(lines, "username: "+toonString(ref.Username))
	}
	if ref.CanonicalURL != "" {
		lines = append(lines, "canonical_url: "+toonString(ref.CanonicalURL))
	}
	return strings.Join(lines, "\n")
}
