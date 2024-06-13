package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"

	"github.com/igolaizola/webcli/pkg/webcobra"
	"github.com/spf13/cobra"
)

// Build flags
var version = ""
var commit = ""
var date = ""

func main() {
	// Create signal based context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Launch commandwebff
	rootCmd := newCommand()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		log.Fatal(err)
	}
}

func newCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "webcobra",
		Short: "webcobra [flags] <subcommand>",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				port, _ := cmd.Flags().GetInt("port")
				s, err := webcobra.New(&webcobra.Config{
					App:      cmd.Name(),
					Commands: cmd.Commands(),
					Address:  fmt.Sprintf(":%d", port),
				})
				if err != nil {
					return err
				}
				return s.Run(cmd.Context())
			}
			return cmd.Help()
		},
	}

	// Disable default commands
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})

	rootCmd.Flags().Int("port", 0, "port number")

	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(newRunCommand())

	return rootCmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			v := version
			if v == "" {
				if buildInfo, ok := debug.ReadBuildInfo(); ok {
					v = buildInfo.Main.Version
				}
			}
			if v == "" {
				v = "dev"
			}
			versionFields := []string{v}
			if commit != "" {
				versionFields = append(versionFields, commit)
			}
			if date != "" {
				versionFields = append(versionFields, date)
			}
			fmt.Println(strings.Join(versionFields, " "))
		},
	}
}

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "webcobra run command",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("running")
			defer log.Println("finished")
			for i := 0; i < 5; i++ {
				select {
				case <-cmd.Context().Done():
				case <-time.After(1 * time.Second):
				}
				fmt.Println("tick", i)
			}
			return nil
		},
	}

	cmd.Flags().String("config", "", "config file (optional)")
	cmd.Flags().Duration("max-duration", 0, "duration")
	cmd.Flags().Int("attempts", 0, "int")
	cmd.Flags().Bool("debug", false, "bool")
	cmd.Flags().Float64("price", 0, "float64")
	cmd.Flags().StringArray("tags", []string{"bar", "foo"}, "tags")

	cmd.AddCommand(newSubRunCommand(cmd.Name()))

	return cmd
}

func newSubRunCommand(parent string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subrun",
		Short: fmt.Sprintf("webcobra %s subrun command", parent),
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Println("running")
			defer log.Println("finished")
			for i := 0; i < 5; i++ {
				select {
				case <-cmd.Context().Done():
				case <-time.After(1 * time.Second):
				}
				fmt.Println("tick", i)
			}
			return nil
		},
	}

	cmd.Flags().String("config", "", "config file (optional)")
	cmd.Flags().Duration("max-duration", 0, "duration")
	cmd.Flags().Int("attempts", 0, "int")
	cmd.Flags().Bool("debug", false, "bool")
	cmd.Flags().Float64("price", 0, "float64")
	cmd.Flags().StringArray("tags", []string{"bar", "foo"}, "tags")

	return cmd
}
