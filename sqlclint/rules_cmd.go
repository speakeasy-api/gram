package main

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/speakeasy-api/gram/sqlclint/catalog"
)

func newRulesCommand() *cli.Command {
	return &cli.Command{
		Name:  "rules",
		Usage: "list every diagnostic and exemption category",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "kind",
				Usage: "restrict the listing to \"diagnostic\" or \"exemption\"",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cat, err := catalog.Load()
			if err != nil {
				return fmt.Errorf("load rule catalog: %w", err)
			}

			rules := cat.All()
			if k := cmd.String("kind"); k != "" {
				kind := catalog.Kind(k)
				if kind != catalog.KindDiagnostic && kind != catalog.KindExemption {
					return fmt.Errorf("unknown kind %q: expected %q or %q", k, catalog.KindDiagnostic, catalog.KindExemption)
				}
				rules = cat.ByKind(kind)
			}

			w := tabwriter.NewWriter(cmd.Root().Writer, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tKIND\tSUMMARY")
			for _, r := range rules {
				fmt.Fprintf(w, "%s\t%s\t%s\n", r.ID, r.Kind, r.Summary)
			}
			if err := w.Flush(); err != nil {
				return fmt.Errorf("write rules table: %w", err)
			}

			fmt.Fprintf(cmd.Root().Writer, "\nRun `sqlclint rule <id>` for the full description.\n")
			return nil
		},
	}
}

func newRuleCommand() *cli.Command {
	return &cli.Command{
		Name:      "rule",
		Usage:     "print the full description of one rule",
		ArgsUsage: "<rule-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cat, err := catalog.Load()
			if err != nil {
				return fmt.Errorf("load rule catalog: %w", err)
			}

			id := cmd.Args().First()
			if id == "" {
				return fmt.Errorf("no rule id given; run `sqlclint rules` to list them")
			}

			rule, ok := cat.Lookup(id)
			if !ok {
				return fmt.Errorf("unknown rule %q; valid ids are:\n  %s", id, strings.Join(allIDs(cat), "\n  "))
			}

			out := cmd.Root().Writer
			fmt.Fprintf(out, "%s (%s)\n%s\n\n", rule.ID, rule.Kind, rule.Summary)
			if len(rule.SilencedBy) > 0 {
				fmt.Fprintf(out, "Silenced by: %s\n\n", strings.Join(rule.SilencedBy, ", "))
			}
			fmt.Fprintln(out, rule.Body)
			return nil
		},
	}
}

func allIDs(cat *catalog.Catalog) []string {
	rules := cat.All()
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.ID)
	}
	return out
}
