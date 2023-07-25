package test

import (
	"fmt"
	"net/http"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
)

type PromMetric struct {
	Category string
	Product  string
	Value    float64
}

func getPromMetrics(bindString string) (*dto.MetricFamily, error) {
	url := fmt.Sprintf("http://localhost%s/metrics", bindString)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	parser := expfmt.TextParser{}
	metrics, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return nil, err
	}
	return metrics["appcat:raw:billing"], nil
}

func AssertPromMetrics(assert *assert.Assertions, assertMetrics []PromMetric, bindString string) error {
	metrics, err := getPromMetrics(bindString)
	if err != nil {
		return err
	}

	assert.Len(metrics.Metric, len(assertMetrics), "not an equal amount of metrics")

	convertedMetrics := map[string]PromMetric{}
	for _, metric := range metrics.Metric {
		product, category := getLabels(metric)
		convertedMetrics[product+category] = PromMetric{
			Product:  product,
			Category: category,
			Value:    *metric.Gauge.Value,
		}
	}

	for _, metric := range assertMetrics {
		m, ok := convertedMetrics[metric.Product+metric.Category]
		assert.True(ok, "metric not in exported list")
		assert.Equal(metric.Value, m.Value, "metric values don't match")
	}

	return nil
}

func getLabels(metric *dto.Metric) (string, string) {
	product := ""
	category := ""

	for _, label := range metric.Label {
		if *label.Name == "product" {
			product = *label.Value
		}
		if *label.Name == "category" {
			category = *label.Value
		}
	}

	return product, category
}
