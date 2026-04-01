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

	root.AddCommand(versionCmd())
	root.AddCommand(setupCmd())
	root.AddCommand(upgradeCmd())
	root.AddCommand(configCmd())
	root.AddCommand(initCmd())
	root.AddCommand(doctorCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("tc %s (built %s)\n", version, buildTime)
		},
	}
}

func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-time configuration",
		Long:  "Guides you through provider selection, API key entry, and model choice.\nCreates ~/.tc/config.toml with secure permissions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.RunSetup()
		},
	}
}

func upgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Check for updates and self-upgrade",
		Long:  "Downloads the latest release from GitHub and performs an atomic binary swap.\nNever leaves you without a working binary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.RunUpgrade(version)
		},
	}
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or edit configuration",
		Long:  "Without arguments, shows the current configuration.\nUse 'get' and 'set' subcommands to modify individual values.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.RunConfigShow()
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "get [key]",
		Short: "Get a config value",
		Long:  "Keys: provider, api_key, model, base_url, ollama_url",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.RunConfigGet(args[0])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set a config value",
		Long:  "Keys: provider, api_key, model, base_url, ollama_url",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.RunConfigSet(args[0], args[1])
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print config file path",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(app.ConfigPath())
		},
	})

	return cmd
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize tc for the current project",
		Long:  "Creates .tc/ directory with a restrictions template and memory store.\nAdds .tc/ to .gitignore.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.RunInit()
		},
	}
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose configuration and system issues",
		Long:  "Checks config validity, API connectivity, kernel security features,\nand project restrictions. Reports pass/warn/fail for each check.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.RunDoctor(version)
		},
	}
}
