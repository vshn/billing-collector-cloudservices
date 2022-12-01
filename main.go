package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/urfave/cli/v2"
)

var (
	// these variables are populated by Goreleaser when releasing
	version = "unknown"
	commit  = "-dirty-"
	date    = time.Now().Format("2006-01-02")

	appName     = "exoscale-metrics-collector"
	appLongName = "Metrics collector which gathers metrics information for exoscale services"
)

func init() {
	// Remove `-v` short option from --version flag
	cli.VersionFlag.(*cli.BoolFlag).Aliases = nil
}

func main() {
	ctx, stop, app := newApp()
	defer stop()
	err := app.RunContext(ctx, os.Args)
	// If required flags aren't set, it will return with error before we could set up logging
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newApp() (context.Context, context.CancelFunc, *cli.App) {
	logInstance := &atomic.Value{}
	logInstance.Store(logr.Discard())
	app := &cli.App{
		Name:    appName,
		Usage:   appLongName,
		Version: fmt.Sprintf("%s, revision=%s, date=%s", version, commit, date),

		EnableBashCompletion: true,

		Before: setupLogging,
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name: "log-level", Aliases: []string{"v"},
				EnvVars: []string{"LOG_LEVEL"},
				Usage:   "number of the log level verbosity",
				Value:   0,
			},
			&cli.StringFlag{
				Name:        "log-format",
				EnvVars:     []string{"LOG_FORMAT"},
				Usage:       "sets the log format (values: [json, console])",
				DefaultText: "console",
			},
		},
		Commands: []*cli.Command{
			newObjectStorageCommand(),
			newDBaasSCommand(),
		},
		ExitErrHandler: func(ctx *cli.Context, err error) {
			if err != nil {
				AppLogger(ctx).Error(err, "fatal error")
				cli.HandleExitCoder(cli.Exit("", 1))
			}
		},
	}
	hasSubcommands := len(app.Commands) > 0
	app.Action = rootAction(hasSubcommands)

	parentCtx := context.WithValue(context.Background(), loggerContextKey{}, logInstance)
	ctx, stop := signal.NotifyContext(parentCtx, syscall.SIGINT, syscall.SIGTERM)
	return ctx, stop, app
}

func rootAction(hasSubcommands bool) func(context *cli.Context) error {
	return func(ctx *cli.Context) error {
		if hasSubcommands {
			return cli.ShowAppHelp(ctx)
		}
		return LogMetadata(ctx)
	}
}
