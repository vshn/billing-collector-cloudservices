package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
	"github.com/vshn/billing-collector-cloudservices/pkg/cmd"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
)

var (
	// these variables are populated by Goreleaser when releasing
	version = "unknown"
	commit  = "-dirty-"
	date    = time.Now().Format("2006-01-02")

	appName     = "billing-collector-cloudservices"
	appLongName = "Metrics collector which gathers metrics information for cloud services"
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
	var (
		logLevel  int
		logFormat string
	)
	app := &cli.App{
		Name:    appName,
		Usage:   appLongName,
		Version: fmt.Sprintf("%s, revision=%s, date=%s", version, commit, date),

		EnableBashCompletion: true,

		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:        "log-level",
				Aliases:     []string{"v"},
				EnvVars:     []string{"LOG_LEVEL"},
				Usage:       "number of the log level verbosity",
				Value:       0,
				Destination: &logLevel,
			},
			&cli.StringFlag{
				Name:        "log-format",
				EnvVars:     []string{"LOG_FORMAT"},
				Usage:       "sets the log format (values: [json, console])",
				DefaultText: "console",
				Destination: &logFormat,
			},
			&cli.IntFlag{
				Name:  "collectInterval",
				Usage: "Interval in which the exporter checks the cloud resources",
				Value: 10,
			},
			&cli.IntFlag{
				Name:  "billingHour",
				Usage: "After which hour every day the objectstorage collector should start",
				Value: 6,
				Action: func(c *cli.Context, i int) error {
					if i > 23 || i < 0 {
						return fmt.Errorf("invalid billingHour value, needs to be between 0 and 23")
					}
					return nil
				},
			},
			&cli.StringFlag{
				Name:  "organizationOverride",
				Usage: "If the collector is collecting the metrics for an APPUiO managed instance. It needs to set the name of the customer.",
				Value: "",
			},
			&cli.StringFlag{
				Name:  "bind",
				Usage: "Golang bind string. Will be used for the exporter",
				Value: ":9123",
			},
		},
		Before: func(c *cli.Context) error {
			logger, err := log.NewLogger(appName, version, logLevel, logFormat)
			if err != nil {
				return fmt.Errorf("before: %w", err)
			}
			c.Context = log.NewLoggingContext(c.Context, logger)
			log.Logger(c.Context).WithValues(
				"date", date,
				"commit", commit,
				"go_os", runtime.GOOS,
				"go_arch", runtime.GOARCH,
				"go_version", runtime.Version(),
				"uid", os.Getuid(),
				"gid", os.Getgid(),
			).Info("Starting up " + appName)
			return nil
		},
		Action: func(c *cli.Context) error {
			if true {
				return cli.ShowAppHelp(c)
			}

			return nil
		},
		Commands: []*cli.Command{
			cmd.ExoscaleCmds(),
			cmd.CloudscaleCmds(),
		},
		ExitErrHandler: func(c *cli.Context, err error) {
			if err != nil {
				log.Logger(c.Context).Error(err, "fatal error")
				cli.HandleExitCoder(cli.Exit("", 1))
			}
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	return ctx, stop, app
}
