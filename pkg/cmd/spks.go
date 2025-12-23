package cmd

import (
	"context"
	"fmt"

	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/urfave/cli/v2"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/odoo"
)

var (
	prometheusQueryArr = [2]string{
		"count(max_over_time(crossplane_resource_info{kind=\"compositemariadbinstances\", service_level=\"%s\"}[1d:1d]))",
		"count(max_over_time(crossplane_resource_info{kind=\"compositeredisinstances\", service_level=\"%s\"}[1d:1d]))",
	}

	odooURL           string
	odooOauthTokenURL string
	odooClientId      string
	odooClientSecret  string
	salesOrder        string
	prometheusURL     string
	unitID            string
	environment       string
	serviceSLA        string
	days              int
)

func SpksCMD(allMetrics map[string]map[string]prometheus.Counter, ctx context.Context) *cli.Command {

	return &cli.Command{
		Name:   "spks",
		Usage:  "Collect metrics from spks.",
		Before: addCommandName,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "odoo-url", Usage: "URL of the Odoo Metered Billing API",
				EnvVars: []string{"ODOO_URL"}, Destination: &odooURL, Value: "https://preprod.central.vshn.ch/api/v2/product_usage_report_POST"},
			&cli.StringFlag{Name: "odoo-oauth-token-url", Usage: "Oauth Token URL to authenticate with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_TOKEN_URL"}, Destination: &odooOauthTokenURL, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "odoo-oauth-client-id", Usage: "Client ID of the oauth client to interact with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_CLIENT_ID"}, Destination: &odooClientId, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "odoo-oauth-client-secret", Usage: "Client secret of the oauth client to interact with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_CLIENT_SECRET"}, Destination: &odooClientSecret, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "sales-order", Usage: "Sales order to report billing data to",
				EnvVars: []string{"SALES_ORDER"}, Destination: &salesOrder, Required: false, DefaultText: defaultTextForOptionalFlags, Value: "S10121"},
			&cli.StringFlag{Name: "prometheus-url", Usage: "URL of the Prometheus API",
				EnvVars: []string{"PROMETHEUS_URL"}, Destination: &prometheusURL, Required: false, DefaultText: defaultTextForRequiredFlags, Value: "http://prometheus-monitoring-application.monitoring-application.svc.cluster.local:9090"},
			&cli.StringFlag{Name: "unit-id", Usage: "Metered Billing UoM ID for the consumed units",
				EnvVars: []string{"UNIT_ID"}, Destination: &unitID, Required: false, DefaultText: defaultTextForRequiredFlags, Value: "uom_uom_68_b1811ca1"},
			&cli.StringFlag{Name: "environment", Usage: "Environment of the instances (eg. nonprod, prod)",
				EnvVars: []string{"ENVIRONMENT"}, Destination: &environment, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "service-sla", Usage: "The sla of the instances on the cluster (\"standard\" or \"premium\")",
				EnvVars: []string{"SERVICE_SLA"}, Destination: &serviceSLA, Required: false, DefaultText: defaultTextForOptionalFlags, Value: "standard"},
			&cli.IntFlag{Name: "days", Usage: "Days of metrics to fetch since today, set to 0 to get current metrics",
				EnvVars: []string{"DAYS"}, Destination: &days, Value: 0, Required: false, DefaultText: defaultTextForOptionalFlags},
		},
		Action: func(c *cli.Context) error {
			ctxx, cancel := context.WithCancel(ctx)
			defer cancel()
			logger := log.Logger(c.Context)
			logger.Info("starting spks data collector")

			ticker := time.NewTicker(24 * time.Hour)

			daysChannel := make(chan int, 1)
			if days != 0 {
				daysChannel <- days
			} else {
				runSPKSBilling(logger, allMetrics, c.Context)
			}

			for {
				select {
				case <-ctxx.Done():
					logger.Info("Received Context cancellation, exiting...")
					return nil
				case <-ticker.C:
					// this runs every 24 hours after program start
					runSPKSBilling(logger, allMetrics, c.Context)
				case <-daysChannel:
					runSPKSBilling(logger, allMetrics, c.Context)
					if days > 0 {
						days--
						daysChannel <- days
					}
				}
			}
		},
	}
}

func runSPKSBilling(logger logr.Logger, allMetrics map[string]map[string]prometheus.Counter, c context.Context) {
	// var startYesterdayAbsolute time.Time
	location, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		allMetrics["odooMetrics"]["odooFailed"].Inc()
	}
	now := time.Now().In(location)
	// this variable is necessary to query Prometheus, with timerange [1d:1d] it returns data from 1 day up to midnight
	startOfToday := time.Date(now.Year(), now.Month(), now.Day()-days, 0, 0, 0, 0, location)
	startYesterdayAbsolute := time.Date(now.Year(), now.Month(), now.Day()-days-1, 0, 0, 0, 0, location).In(time.UTC)

	endYesterdayAbsolute := startYesterdayAbsolute.Add(24 * time.Hour)

	logger.Info("Running SPKS billing with such timeranges: ", "startOfToday", startOfToday, "startYesterdayAbsolute", startYesterdayAbsolute.Local(), "endYesterdayAbsolute", endYesterdayAbsolute.Local())

	odooClient := odoo.NewOdooAPIClient(c, odooURL, odooOauthTokenURL, odooClientId, odooClientSecret, logger, allMetrics["odooMetrics"])

	mariadb, redis, err := getDatabasesCounts(logger, startOfToday, allMetrics)
	if err != nil {
		logger.Error(err, "Error getting database counts")
	}

	billingRecords := generateBillingRecords(startYesterdayAbsolute, endYesterdayAbsolute, mariadb, redis)

	err = odooClient.SendData(billingRecords)
	if err != nil {
		logger.Error(err, "Error sending data to Odoo API")
	}
}

func generateBillingRecords(startYesterdayAbsolute time.Time, endYesterdayAbsolute time.Time, mariadb int, redis int) []odoo.OdooMeteredBillingRecord {
	timerange := odoo.TimeRange{
		From: startYesterdayAbsolute,
		To:   endYesterdayAbsolute,
	}

	billingRecords := []odoo.OdooMeteredBillingRecord{
		{
			ProductID:     "appcat-spks-mariadb-" + serviceSLA,
			InstanceID:    "mariadb-" + environment,
			SalesOrder:    salesOrder,
			UnitID:        unitID,
			ConsumedUnits: float64(mariadb),
			TimeRange:     timerange,
		},
		{
			ProductID:     "appcat-spks-redis-" + serviceSLA,
			InstanceID:    "redis-" + environment,
			SalesOrder:    salesOrder,
			UnitID:        unitID,
			ConsumedUnits: float64(redis),
			TimeRange:     timerange,
		},
	}

	return billingRecords
}

func getDatabasesCounts(logger logr.Logger, startOfToday time.Time, allMetrics map[string]map[string]prometheus.Counter) (int, int, error) {

	client, err := api.NewClient(api.Config{
		Address: prometheusURL,
	})
	if err != nil {
		logger.Error(err, "Error creating Prometheus client")
	}

	v1api := v1.NewAPI(client)
	ctxx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mariadb, err := QueryPrometheus(ctxx, v1api, fmt.Sprintf(prometheusQueryArr[0], serviceSLA), logger, startOfToday, allMetrics["providerMetrics"])
	if err != nil {
		return -1, -1, err
	}

	redis, err := QueryPrometheus(ctxx, v1api, fmt.Sprintf(prometheusQueryArr[1], serviceSLA), logger, startOfToday, allMetrics["providerMetrics"])
	if err != nil {
		return -1, -1, err
	}

	return mariadb, redis, nil
}

func QueryPrometheus(ctx context.Context, v1api v1.API, query string, logger logr.Logger, absoluteBeginningTime time.Time, providerMetrics map[string]prometheus.Counter) (int, error) {
	result, warnings, err := v1api.Query(ctx, query, absoluteBeginningTime, v1.WithTimeout(5*time.Second))
	if err != nil {
		providerMetrics["providerFailed"].Inc()
		logger.Error(err, "Error querying Prometheus")
		return -1, err
	}

	providerMetrics["providerSucceeded"].Inc()

	if len(warnings) > 0 {
		logger.Info("Warnings", "warnings from Prometheus query", warnings)
	}

	switch result.Type() {
	case model.ValVector:
		vectorVal := result.(model.Vector)
		if len(vectorVal) != 1 {
			return 0, nil
		}
	default:
		logger.Error(err, "result type is not Vector: ", "result", result)
		providerMetrics["providerFailed"].Inc()
		return -1, err

	}
	return int(result.(model.Vector)[0].Value), nil
}
