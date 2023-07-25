package prom

import (
	"context"
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
)

var (
	gauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "appcat:raw:billing",
		Help: "AppCat raw billing metrics, not intended for direct use in billing",
	}, []string{
		"category",
		"product",
		"type",
	})
)

const defaultBind = ":9123"

// ResetAppCatMetric makes sure that old metrics are removed from the metrics list.
func ResetAppCatMetric() {
	gauge.Reset()
}

// UpdateAppCatMetric resets the metrics and exports new values.
func UpdateAppCatMetric(metric float64, category, product, prodType string) {
	gauge.WithLabelValues(category, product, prodType).Set(metric)
}

func ServeMetrics(ctx context.Context, bindString string) error {

	if bindString == "" {
		bindString = defaultBind
	}

	log := log.Logger(ctx)

	log.Info("Starting metric http server")

	err := prometheus.Register(gauge)
	if err != nil {
		are := &prometheus.AlreadyRegisteredError{}
		if !errors.As(err, are) {
			return err
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    bindString,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		log.Info("Received shutdown signal for http server")
		err := server.Shutdown(ctx)
		if err != nil {
			log.Error(err, "error stopping http server")
		}
	}()

	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	log.Info("Shutting down metrics exporter")

	return nil
}
