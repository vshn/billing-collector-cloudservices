package cloudscale

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v2"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	cloudscalev1 "github.com/vshn/provider-cloudscale/apis/cloudscale/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	organizationLabel = "appuio.io/organization"
	namespaceLabel    = "crossplane.io/claim-namespace"
)

// AccumulateKey represents one data point ("fact") in the billing database.
// The actual value for the data point is not present in this type, as this type is just a map key, and the corresponding value is stored as a map value.
type AccumulateKey struct {
	Query string
	// Zone is currently always just `cloudscale`
	Zone      string
	Tenant    string
	Namespace string
	Start     time.Time
}

// GetSourceString returns the full "source" string as used by the appuio-cloud-reporting
func (k AccumulateKey) GetSourceString() string {
	return k.Query + ":" + k.Zone + ":" + k.Tenant + ":" + k.Namespace
}

func (k AccumulateKey) GetCategoryString() string {
	return k.Zone + ":" + k.Namespace
}

// MarshalText implements encoding.TextMarshaler to be able to e.g. log the map with this key type.
func (k AccumulateKey) MarshalText() ([]byte, error) {
	return []byte(k.GetSourceString()), nil
}

/*
accumulateBucketMetrics gets all the bucket metrics from cloudscale and puts them into a map. The map key is the "AccumulateKey",
and the value is the raw value of the data returned by cloudscale (e.g. bytes, requests). In order to construct the
correct AccumulateKey, this function needs to fetch the namespace and bucket custom resources, because that's where the tenant is stored.
This method is "accumulating" data because it collects data from possibly multiple ObjectsUsers under the same
AccumulateKey. This is because the billing system can't handle multiple ObjectsUsers per namespace.
*/
func accumulateBucketMetrics(ctx context.Context, date time.Time, cloudscaleClient *cloudscale.Client, k8sclient client.Client, orgOverride string) (map[AccumulateKey]uint64, error) {
	logger := log.Logger(ctx)

	logger.V(1).Info("fetching bucket metrics from cloudscale", "date", date)

	bucketMetricsRequest := cloudscale.BucketMetricsRequest{Start: date, End: date}
	bucketMetrics, err := cloudscaleClient.Metrics.GetBucketMetrics(ctx, &bucketMetricsRequest)
	if err != nil {
		return nil, err
	}

	logger.V(1).Info("fetching namespaces")

	nsTenants, err := kubernetes.FetchNamespaceWithOrganizationMap(ctx, k8sclient, orgOverride)
	if err != nil {
		return nil, err
	}

	logger.V(1).Info("fetching buckets")

	buckets, err := fetchBuckets(ctx, k8sclient)
	if err != nil {
		return nil, err
	}

	accumulated := make(map[AccumulateKey]uint64)

	for _, bucketMetricsData := range bucketMetrics.Data {
		name := bucketMetricsData.Subject.BucketName
		logger := logger.WithValues("bucket", name)
		ns, ok := buckets[name]
		if !ok {
			logger.Info("unable to sync bucket, ObjectBucket not found")
			continue
		}
		tenant, ok := nsTenants[ns]
		if !ok {
			logger.Info("unable to sync bucket, no tenant mapping available for namespace", "namespace", ns)
			continue
		}
		err = accumulateBucketMetricsForObjectsUser(accumulated, bucketMetricsData, tenant, ns)
		if err != nil {
			logger.Error(err, "unable to sync bucket", "namespace", ns)
			continue
		}
		logger.V(1).Info("accumulated raw bucket metrics", "namespace", ns, "tenant", tenant, "accumulated", accumulated)
	}

	return accumulated, nil
}

func fetchBuckets(ctx context.Context, k8sclient client.Client) (map[string]string, error) {
	buckets := &cloudscalev1.BucketList{}
	if err := k8sclient.List(ctx, buckets, client.HasLabels{namespaceLabel}); err != nil {
		return nil, fmt.Errorf("bucket list: %w", err)
	}

	bucketNS := map[string]string{}
	for _, b := range buckets.Items {
		bucketNS[b.GetBucketName()] = b.Labels[namespaceLabel]
	}
	return bucketNS, nil
}

func accumulateBucketMetricsForObjectsUser(accumulated map[AccumulateKey]uint64, bucketMetricsData cloudscale.BucketMetricsData, tenant, namespace string) error {
	if len(bucketMetricsData.TimeSeries) != 1 {
		return fmt.Errorf("there must be exactly one metrics data point, found %d", len(bucketMetricsData.TimeSeries))
	}

	// For now all the buckets have the same zone. This may change in the future if Cloudscale decides to have different
	// prices for different locations.
	zone := sourceZones[0]

	sourceStorage := AccumulateKey{
		Query:     sourceQueryStorage,
		Zone:      zone,
		Tenant:    tenant,
		Namespace: namespace,
		Start:     bucketMetricsData.TimeSeries[0].Start,
	}
	sourceTrafficOut := AccumulateKey{
		Query:     sourceQueryTrafficOut,
		Zone:      zone,
		Tenant:    tenant,
		Namespace: namespace,
		Start:     bucketMetricsData.TimeSeries[0].Start,
	}
	sourceRequests := AccumulateKey{
		Query:     sourceQueryRequests,
		Zone:      zone,
		Tenant:    tenant,
		Namespace: namespace,
		Start:     bucketMetricsData.TimeSeries[0].Start,
	}

	accumulated[sourceStorage] += uint64(bucketMetricsData.TimeSeries[0].Usage.StorageBytes)
	accumulated[sourceTrafficOut] += uint64(bucketMetricsData.TimeSeries[0].Usage.SentBytes)
	accumulated[sourceRequests] += uint64(bucketMetricsData.TimeSeries[0].Usage.Requests)

	return nil
}
