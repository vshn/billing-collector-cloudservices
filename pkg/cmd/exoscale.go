package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
	"github.com/vshn/billing-collector-cloudservices/pkg/exoscale"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/prom"
)

var (
	secret     string
	accessKey  string
	kubeconfig string
)

func addCommandName(c *cli.Context) error {
	c.Context = log.NewLoggingContext(c.Context, log.Logger(c.Context).WithName(c.Command.Name))
	return nil
}

func ExoscaleCmds() *cli.Command {

	return &cli.Command{
		Name:  "exoscale",
		Usage: "Collect metrics from exoscale",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "exoscale-secret",
				Aliases:     []string{"s"},
				EnvVars:     []string{"EXOSCALE_API_SECRET"},
				Required:    true,
				Usage:       "The secret which has unrestricted SOS service access in an Exoscale organization",
				Destination: &secret,
			},
			&cli.StringFlag{
				Name:        "exoscale-access-key",
				Aliases:     []string{"k"},
				EnvVars:     []string{"EXOSCALE_API_KEY"},
				Required:    true,
				Usage:       "A key which has unrestricted SOS service access in an Exoscale organization",
				Destination: &accessKey,
			},
			&cli.StringFlag{
				Name:        "kubeconfig",
				EnvVars:     []string{"KUBECONFIG"},
				Usage:       "Path to a kubeconfig file which will be used instead of url/token flags if set",
				Destination: &kubeconfig,
			},
		},
		Before: addCommandName,
		Action: collectAllExo,
	}
}

func collectAllExo(c *cli.Context) error {
	logger := log.Logger(c.Context)

	logger.Info("Creating Exoscale client")
	exoscaleClient, err := exoscale.NewClient(accessKey, secret)
	if err != nil {
		return fmt.Errorf("exoscale client: %w", err)
	}

	logger.Info("Creating k8s client")
	k8sClient, err := kubernetes.NewClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("k8s client: %w", err)
	}

	orgOverride := strings.ToLower(c.String("organizationOverride"))

	d, err := exoscale.NewDBaaS(exoscaleClient, k8sClient, orgOverride)
	if err != nil {
		return fmt.Errorf("dbaas service: %w", err)
	}

	o, err := exoscale.NewObjectStorage(exoscaleClient, k8sClient, orgOverride)
	if err != nil {
		return fmt.Errorf("objectbucket service: %w", err)
	}

	billingHour := c.Int("billingHour")
	collectInterval := c.Int("collectInterval")

	go func() {
		for {
			metrics, err := d.Accumulate(c.Context)
			if err != nil {
				logger.Error(err, "cannot execute exoscale dbaas collector")
				os.Exit(1)
			}

			logger.Info("Collecting ObjectStorage metrics after", "hour", billingHour)
			if time.Now().Hour() >= billingHour {
				buckets, err := o.Accumulate(c.Context, billingHour)
				if err != nil {
					logger.Error(err, "cannot execute objectstorage collector")
					os.Exit(1)
				}
				metrics = mergeMaps(metrics, buckets)
			}

			err = exoscale.Export(metrics)
			if err != nil {
				logger.Error(err, "cannot export metrics")
			}

			time.Sleep(time.Second * time.Duration(collectInterval))
		}
	}()

	return prom.ServeMetrics(c.Context, c.String("bind"))
}

func mergeMaps(m1, m2 map[exoscale.Key]exoscale.Aggregated) map[exoscale.Key]exoscale.Aggregated {
	merged := map[exoscale.Key]exoscale.Aggregated{}
	for k, v := range m1 {
		merged[k] = v
	}
	for k, v := range m2 {
		merged[k] = v
	}
	return merged
}
