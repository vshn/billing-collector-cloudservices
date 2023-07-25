package cloudscale

import (
	"context"
	"errors"
	"time"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v2"
	"github.com/vshn/billing-collector-cloudservices/pkg/prom"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ObjectStorage struct {
	client      *cloudscale.Client
	k8sClient   client.Client
	orgOverride string
}

func NewObjectStorage(client *cloudscale.Client, k8sClient client.Client, orgOverride string) (*ObjectStorage, error) {
	return &ObjectStorage{
		client:      client,
		k8sClient:   k8sClient,
		orgOverride: orgOverride,
	}, nil
}

func (obj *ObjectStorage) Accumulate(ctx context.Context, date time.Time) (map[AccumulateKey]uint64, error) {
	return accumulateBucketMetrics(ctx, date, obj.client, obj.k8sClient, obj.orgOverride)
}

func convertUnit(unit string, value uint64) (float64, error) {
	if unit == "GB" || unit == "GBDay" {
		return float64(value) / 1000 / 1000 / 1000, nil
	}
	if unit == "KReq" {
		return float64(value) / 1000, nil
	}
	return 0, errors.New("Unknown query unit " + unit)
}

func Export(metrics map[AccumulateKey]uint64, billingHour int) error {
	prom.ResetAppCatMetric()

	billingParts := 24 - billingHour

	for k, v := range metrics {
		value, err := convertUnit(units[k.Query], v)
		if err != nil {
			return err
		}
		value = value / float64(billingParts)
		prom.UpdateAppCatMetric(value, k.GetCategoryString(), k.GetSourceString(), "objectStorage")
	}
	return nil
}
