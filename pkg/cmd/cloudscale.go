package cmd

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v2"
	"github.com/urfave/cli/v2"
	cs "github.com/vshn/billing-collector-cloudservices/pkg/cloudscale"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/prom"
)

func CloudscaleCmds() *cli.Command {
	var (
		apiToken   string
		kubeconfig string
		days       int
	)
	return &cli.Command{
		Name:  "cloudscale",
		Usage: "Collect metrics from cloudscale",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "cloudscale-api-token",
				EnvVars:     []string{"CLOUDSCALE_API_TOKEN"},
				Required:    true,
				Usage:       "API token for cloudscale",
				Destination: &apiToken,
			},
			&cli.StringFlag{
				Name:        "kubeconfig",
				EnvVars:     []string{"KUBECONFIG"},
				Usage:       "Path to a kubeconfig file which will be used instead of url/token flags if set",
				Destination: &kubeconfig,
			},
			&cli.IntFlag{
				Name:        "days",
				EnvVars:     []string{"DAYS"},
				Value:       1,
				Usage:       "Days of metrics to fetch since today, set to 0 to get current metrics",
				Destination: &days,
			},
		},
		Before: addCommandName,
		Action: func(c *cli.Context) error {
			logger := log.Logger(c.Context)

			logger.Info("Creating cloudscale client")
			cloudscaleClient := cloudscale.NewClient(http.DefaultClient)
			cloudscaleClient.AuthToken = apiToken

			logger.Info("Creating k8s client")
			k8sClient, err := kubernetes.NewClient(kubeconfig)
			if err != nil {
				return fmt.Errorf("k8s client: %w", err)
			}

			location, err := time.LoadLocation("Europe/Zurich")
			if err != nil {
				return fmt.Errorf("load loaction: %w", err)
			}

			orgOverride := strings.ToLower(c.String("organizationOverride"))

			o, err := cs.NewObjectStorage(cloudscaleClient, k8sClient, orgOverride)
			if err != nil {
				return fmt.Errorf("object storage: %w", err)
			}

			collectInterval := c.Int("collectInterval")
			billingHour := c.Int("billingHour")
			go func() {
				for {
					if time.Now().Hour() >= billingHour {

						billingDate := time.Now().In(location)
						if days != 0 {
							billingDate = time.Date(billingDate.Year(), billingDate.Month(), billingDate.Day()-days, 0, 0, 0, 0, billingDate.Location())
						}

						logger.V(1).Info("Running cloudscale collector")
						collected, err := o.Accumulate(c.Context, billingDate)
						if err != nil {
							logger.Error(err, "could not collect cloudscale bucket metrics")
							os.Exit(1)
						}

						logger.Info("Exporting bucket metrics", "billingHour", billingHour, "date", billingDate)
						err = cs.Export(collected, billingHour)
						if err != nil {
							logger.Error(err, "could not export cloudscale bucket metrics")
						}
					}

					time.Sleep(time.Second * time.Duration(collectInterval))
				}
			}()

			return prom.ServeMetrics(c.Context, c.String("bind"))
		},
	}
}
