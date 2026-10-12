package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vshn/billing-collector-cloudservices/pkg/odoo"

	"github.com/urfave/cli/v2"
	"github.com/vshn/billing-collector-cloudservices/pkg/exoscale"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
)

func addCommandName(c *cli.Context) error {
	c.Context = log.NewLoggingContext(c.Context, log.Logger(c.Context).WithName(c.Command.Name))
	return nil
}

func ExoscaleCmds(allMetrics map[string]map[string]prometheus.Counter) *cli.Command {
	var (
		secret            string
		accessKey         string
		kubeconfig        string
		controlApiUrl     string
		controlApiToken   string
		odooURL           string
		odooOauthTokenURL string
		odooClientId      string
		odooClientSecret  string
		salesOrder        string
		clusterId         string
		cloudZone         string
		uom               string
		collectInterval   int
		billingHour       int
	)
	return &cli.Command{
		Name:  "exoscale",
		Usage: "Collect metrics from exoscale",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "exoscale-secret", Aliases: []string{"s"}, Usage: "The secret which has unrestricted SOS service access in an Exoscale organization",
				EnvVars: []string{"EXOSCALE_API_SECRET"}, Destination: &secret, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "exoscale-access-key", Aliases: []string{"k"}, Usage: "A key which has unrestricted SOS service access in an Exoscale organization",
				EnvVars: []string{"EXOSCALE_API_KEY"}, Destination: &accessKey, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "kubeconfig", Usage: "Path to a kubeconfig file which will be used instead of url/token flags if set",
				EnvVars: []string{"KUBECONFIG"}, Destination: &kubeconfig, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "control-api-url", Usage: "URL of the APPUiO Cloud Control API",
				EnvVars: []string{"CONTROL_API_URL"}, Destination: &controlApiUrl, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "control-api-token", Usage: "Token of the APPUiO Cloud Control API",
				EnvVars: []string{"CONTROL_API_TOKEN"}, Destination: &controlApiToken, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "odoo-url", Usage: "URL of the Odoo Metered Billing API",
				EnvVars: []string{"ODOO_URL"}, Destination: &odooURL, Value: "http://localhost:8080"},
			&cli.StringFlag{Name: "odoo-oauth-token-url", Usage: "Oauth Token URL to authenticate with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_TOKEN_URL"}, Destination: &odooOauthTokenURL, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "odoo-oauth-client-id", Usage: "Client ID of the oauth client to interact with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_CLIENT_ID"}, Destination: &odooClientId, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "odoo-oauth-client-secret", Usage: "Client secret of the oauth client to interact with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_CLIENT_SECRET"}, Destination: &odooClientSecret, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "appuio-managed-sales-order", Usage: "Sales order for APPUiO Managed clusters",
				EnvVars: []string{"APPUIO_MANAGED_SALES_ORDER"}, Destination: &salesOrder, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.IntFlag{Name: "collect-interval", Usage: "How often to collect the metrics from the Cloud Service in hours - 1-23",
				EnvVars: []string{"COLLECT_INTERVAL"}, Destination: &collectInterval, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.IntFlag{Name: "billing-hour", Usage: "At what time to start collect the metrics (ex 6 would start running from 6)",
				EnvVars: []string{"BILLING_HOUR"}, Destination: &billingHour, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "cluster-id", Usage: "The cluster id to save in the billing record",
				EnvVars: []string{"CLUSTER_ID"}, Destination: &clusterId, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "cluster-zone", Usage: "The cluster zone to save in the billing record",
				EnvVars: []string{"CLOUD_ZONE"}, Destination: &cloudZone, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "uom", Usage: "Unit of measure mapping between cloud services and Odoo16 in json format",
				EnvVars: []string{"UOM"}, Destination: &uom, Required: true, DefaultText: defaultTextForRequiredFlags},
		},
		Before: addCommandName,
		Subcommands: []*cli.Command{
			{
				Name:   "objectstorage",
				Usage:  "Get metrics from object storage service",
				Before: addCommandName,
				Action: func(c *cli.Context) error {
					logger := log.Logger(c.Context)

					var wg sync.WaitGroup
					logger.Info("Creating Exoscale client")
					exoscaleClient, err := exoscale.NewClient(accessKey, secret)
					if err != nil {
						return fmt.Errorf("exoscale client: %w", err)
					}

					logger.Info("Checking UOM mappings")
					mapping, err := odoo.LoadUOM(uom)
					if err != nil {
						return err
					}
					err = exoscale.CheckObjectStorageUOMExistence(mapping)
					if err != nil {
						return err
					}

					logger.Info("Creating k8s client")
					k8sClient, err := kubernetes.NewClient(kubeconfig, "", "")
					if err != nil {
						return fmt.Errorf("k8s client: %w", err)
					}

					k8sControlClient, err := kubernetes.NewClient("", controlApiUrl, controlApiToken)
					if err != nil {
						return fmt.Errorf("k8s control client: %w", err)
					}

					odooClient := odoo.NewOdooAPIClient(c.Context, odooURL, odooOauthTokenURL, odooClientId, odooClientSecret, logger, allMetrics["odooMetrics"])

					if collectInterval < 1 || collectInterval > 23 {
						// Set to run once a day after billingHour in case the collectInterval is out of boundaries
						collectInterval = 23
					}

					o, err := exoscale.NewObjectStorage(exoscaleClient, k8sClient, k8sControlClient, salesOrder, clusterId, cloudZone, mapping, allMetrics["providerMetrics"])
					if err != nil {
						return fmt.Errorf("objectbucket service: %w", err)
					}

					wg.Add(1)
					go func() {
						for {
							if time.Now().Hour() >= billingHour {

								logger.Info("Collecting ObjectStorage metrics after", "hour", billingHour)

								metrics, err := o.GetMetrics(c.Context)
								if err != nil {
									logger.Error(err, "cannot execute objectstorage collector")
									wg.Done()
								}
								if len(metrics) == 0 {
									logger.Info("No data to export to odoo")
									time.Sleep(time.Hour)
									continue
								}
								logger.Info("Exporting data to Odoo", "time", time.Now())
								err = odooClient.SendData(metrics)
								if err != nil {
									logger.Error(err, "cannot export metrics")
								}
								time.Sleep(time.Hour * time.Duration(collectInterval))
							}
							time.Sleep(time.Hour)
						}
					}()

					wg.Wait()
					os.Exit(1)
					return nil
				},
			},
		},
	}
}
