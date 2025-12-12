package cmd

import (
	"context"

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
	prometheusQueryArr = [4]string{
		"count(max_over_time(crossplane_resource_info{kind=\"compositemariadbinstances\", service_level=\"standard\"}[1d:1d]))",
		"count(max_over_time(crossplane_resource_info{kind=\"compositemariadbinstances\", service_level=\"premium\"}[1d:1d]))",
		"count(max_over_time(crossplane_resource_info{kind=\"compositeredisinstances\", service_level=\"standard\"}[1d:1d]))",
		"count(max_over_time(crossplane_resource_info{kind=\"compositeredisinstances\", service_level=\"premium\"}[1d:1d]))",
	}
	odooURL           string
	odooOauthTokenURL string
	odooClientId      string
	odooClientSecret  string
	salesOrder        string
	prometheusURL     string
	UnitID            string
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
				EnvVars: []string{"UNIT_ID"}, Destination: &UnitID, Required: false, DefaultText: defaultTextForRequiredFlags, Value: "uom_uom_68_b1811ca1"},
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
				runSPKSBilling(prometheusURL, prometheusQueryArr, logger, allMetrics, salesOrder, UnitID, c.Context)
			}

			for {
				select {
				case <-ctxx.Done():
					logger.Info("Received Context cancellation, exiting...")
					return nil
				case <-ticker.C:
					// this runs every 24 hours after program start
					runSPKSBilling(prometheusURL, prometheusQueryArr, logger, allMetrics, salesOrder, UnitID, c.Context)
				case <-daysChannel:
					runSPKSBilling(prometheusURL, prometheusQueryArr, logger, allMetrics, salesOrder, UnitID, c.Context)
					if days > 0 {
						days--
						daysChannel <- days
					}
				}
			}
		},
	}
}

func runSPKSBilling(prometheusURL string, prometheusQueryArr [4]string, logger logr.Logger, allMetrics map[string]map[string]prometheus.Counter, salesOrder string, UnitID string, c context.Context) {
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

	mariadbStandard, mariadbPremium, redisStandard, redisPremium, err := getDatabasesCounts(prometheusURL, prometheusQueryArr, logger, startOfToday, allMetrics)
	if err != nil {
		logger.Error(err, "Error getting database counts")
	}

	billingRecords := generateBillingRecords(salesOrder, UnitID, startYesterdayAbsolute, endYesterdayAbsolute, mariadbStandard, mariadbPremium, redisStandard, redisPremium)

	err = odooClient.SendData(billingRecords)
	if err != nil {
		logger.Error(err, "Error sending data to Odoo API")
	}
}

func generateBillingRecords(salesOrder string, UnitID string, startYesterdayAbsolute time.Time, endYesterdayAbsolute time.Time, mariadbStandard int, mariadbPremium int, redisStandard int, redisPremium int) []odoo.OdooMeteredBillingRecord {
	timerange := odoo.TimeRange{
		From: startYesterdayAbsolute,
		To:   endYesterdayAbsolute,
	}

	billingRecords := []odoo.OdooMeteredBillingRecord{
		{
			ProductID:     "appcat-spks-mariadb-standard",
			InstanceID:    "mariadb-standard",
			SalesOrder:    salesOrder,
			UnitID:        UnitID,
			ConsumedUnits: float64(mariadbStandard),
			TimeRange:     timerange,
		},
		{
			ProductID:     "appcat-spks-mariadb-premium",
			InstanceID:    "mariadb-premium",
			SalesOrder:    salesOrder,
			UnitID:        UnitID,
			ConsumedUnits: float64(mariadbPremium),
			TimeRange:     timerange,
		},
		{
			ProductID:     "appcat-spks-redis-standard",
			InstanceID:    "redis-standard",
			SalesOrder:    salesOrder,
			UnitID:        UnitID,
			ConsumedUnits: float64(redisStandard),
			TimeRange:     timerange,
		},
		{
			ProductID:     "appcat-spks-redis-premium",
			InstanceID:    "redis-premium",
			SalesOrder:    salesOrder,
			UnitID:        UnitID,
			ConsumedUnits: float64(redisPremium),
			TimeRange:     timerange,
		},
	}

	return billingRecords
}

func getDatabasesCounts(prometheusURL string, prometheusQueryArr [4]string, logger logr.Logger, startOfToday time.Time, allMetrics map[string]map[string]prometheus.Counter) (int, int, int, int, error) {

	client, err := api.NewClient(api.Config{
		Address: prometheusURL,
	})
	if err != nil {
		logger.Error(err, "Error creating Prometheus client")
	}

	v1api := v1.NewAPI(client)
	ctxx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mariadbStandard, err := QueryPrometheus(ctxx, v1api, prometheusQueryArr[0], logger, startOfToday, allMetrics["providerMetrics"])
	if err != nil {
		return -1, -1, -1, -1, err
	}

	mariadbPremium, err := QueryPrometheus(ctxx, v1api, prometheusQueryArr[1], logger, startOfToday, allMetrics["providerMetrics"])
	if err != nil {
		return -1, -1, -1, -1, err
	}

	redisStandard, err := QueryPrometheus(ctxx, v1api, prometheusQueryArr[2], logger, startOfToday, allMetrics["providerMetrics"])
	if err != nil {
		return -1, -1, -1, -1, err
	}

	redisPremium, err := QueryPrometheus(ctxx, v1api, prometheusQueryArr[3], logger, startOfToday, allMetrics["providerMetrics"])
	if err != nil {
		return -1, -1, -1, -1, err
	}

	return mariadbStandard, mariadbPremium, redisStandard, redisPremium, nil
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
