package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Hitesh-K-Murali/terminal-code/internal/app"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	root := &cobra.Command{
		Use:   "tc",
		Short: "Terminal AI coding assistant with kernel-level security",
		Long: `tc is a terminal-native AI coding assistant that enforces security
restrictions at the kernel level using seccomp, Landlock, namespaces,
and cgroups. Multi-provider (Claude, OpenAI, Ollama) with intelligent routing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.Run(cmd.Context())
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("tc %s (built %s)\n", version, buildTime)
		},
	})

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
