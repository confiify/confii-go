package cmd

import (
	"fmt"

	"github.com/confiify/confii-go/diff"
	"github.com/spf13/cobra"
)

// NewDiffCmd creates the 'diff' command.
func NewDiffCmd() *cobra.Command {
	var loaders1, loaders2 []string
	var format string

	cmd := &cobra.Command{
		Use:   "diff <env1> <env2>",
		Short: "Compare two configurations",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			env1, env2 := args[0], args[1]

			// Use same loaders for both if loaders2 not specified.
			if len(loaders2) == 0 {
				loaders2 = loaders1
			}

			cfg1, err := buildConfig(env1, loaders1)
			if err != nil {
				return fmt.Errorf("config1: %w", err)
			}

			cfg2, err := buildConfig(env2, loaders2)
			if err != nil {
				return fmt.Errorf("config2: %w", err)
			}

			diffs := diff.Diff(cfg1.ToDict(), cfg2.ToDict())

			if len(diffs) == 0 {
				fmt.Println("No differences found.")
				return nil
			}

			switch format {
			case "json":
				s, _ := diff.ToJSON(diffs)
				fmt.Println(s)
			default:
				for _, d := range diffs {
					printDiff(d, "")
				}
			}

			summary := diff.Summary(diffs)
			fmt.Printf("\nSummary: %d changes (%d added, %d removed, %d modified)\n",
				summary["total"], summary["added"], summary["removed"], summary["modified"])
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&loaders1, "loader1", nil, "Loaders for first config (type:source)")
	cmd.Flags().StringSliceVar(&loaders2, "loader2", nil, "Loaders for second config (type:source)")
	cmd.Flags().StringVarP(&format, "format", "f", "unified", "Output format (unified, json)")
	return cmd
}

func printDiff(d diff.ConfigDiff, indent string) {
	switch d.Type {
	case diff.Added:
		fmt.Printf("%s+ %s = %v\n", indent, d.Path, d.NewValue)
	case diff.Removed:
		fmt.Printf("%s- %s = %v\n", indent, d.Path, d.OldValue)
	case diff.Modified:
		if len(d.NestedDiffs) > 0 {
			fmt.Printf("%s~ %s:\n", indent, d.Path)
			for _, nd := range d.NestedDiffs {
				printDiff(nd, indent+"  ")
			}
		} else {
			fmt.Printf("%s~ %s: %v → %v\n", indent, d.Path, d.OldValue, d.NewValue)
		}
	}
}
