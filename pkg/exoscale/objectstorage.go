package exoscale

import (
	"context"
	"fmt"

	"github.com/vshn/billing-collector-cloudservices/pkg/exofixtures"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"

	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/exoscale/egoscale/v2/oapi"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"

	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectStorage gathers bucket data from exoscale provider and cluster and saves to the database
type ObjectStorage struct {
	k8sClient      k8s.Client
	exoscaleClient *egoscale.Client
	orgOverride    string
}

// BucketDetail a k8s bucket object with relevant data
type BucketDetail struct {
	Organization, BucketName, Namespace string
}

// NewObjectStorage creates an ObjectStorage with the initial setup
func NewObjectStorage(exoscaleClient *egoscale.Client, k8sClient k8s.Client, orgOverride string) (*ObjectStorage, error) {
	return &ObjectStorage{
		exoscaleClient: exoscaleClient,
		k8sClient:      k8sClient,
		orgOverride:    orgOverride,
	}, nil
}

func (o *ObjectStorage) Accumulate(ctx context.Context, billingHour int) (map[Key]Aggregated, error) {
	detail, err := o.fetchManagedBucketsAndNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchManagedBucketsAndNamespaces: %w", err)
	}
	aggregated, err := o.getBucketUsage(ctx, detail, billingHour)
	if err != nil {
		return nil, fmt.Errorf("getBucketUsage: %w", err)
	}
	return aggregated, nil
}

// getBucketUsage gets bucket usage from Exoscale and matches them with the bucket from the cluster
// If there are no buckets in Exoscale, the API will return an empty slice
func (o *ObjectStorage) getBucketUsage(ctx context.Context, bucketDetails []BucketDetail, billingHour int) (map[Key]Aggregated, error) {
	logger := log.Logger(ctx)
	logger.Info("Fetching bucket usage from Exoscale")

	billingParts := 24 - billingHour

	logger.V(1).Info("calculated billing parts", "billingParts", billingParts)

	resp, err := o.exoscaleClient.ListSosBucketsUsageWithResponse(ctx)
	if err != nil {
		return nil, err
	}

	aggregatedBuckets := getAggregatedBuckets(ctx, *resp.JSON200.SosBucketsUsage, bucketDetails, billingParts)
	if len(aggregatedBuckets) == 0 {
		logger.Info("There are no bucket usage to be exported")
		return nil, nil
	}

	return aggregatedBuckets, nil
}

func getAggregatedBuckets(ctx context.Context, sosBucketsUsage []oapi.SosBucketUsage, bucketDetails []BucketDetail, billingParts int) map[Key]Aggregated {
	logger := log.Logger(ctx)
	logger.Info("Aggregating buckets by namespace")

	sosBucketsUsageMap := make(map[string]oapi.SosBucketUsage, len(sosBucketsUsage))
	for _, usage := range sosBucketsUsage {
		sosBucketsUsageMap[*usage.Name] = usage
	}

	aggregatedBuckets := make(map[Key]Aggregated)
	for _, bucketDetail := range bucketDetails {
		logger.V(1).Info("Checking bucket", "bucket", bucketDetail.BucketName)

		if bucketUsage, exists := sosBucketsUsageMap[bucketDetail.BucketName]; exists {
			logger.V(1).Info("Found exoscale bucket usage", "bucket", bucketUsage.Name, "bucket size", bucketUsage.Name)
			key := NewKey(bucketDetail.Namespace)
			aggregatedBucket := aggregatedBuckets[key]
			aggregatedBucket.Key = key
			aggregatedBucket.Source = exofixtures.SOSSourceString{
				Namespace:    bucketDetail.Namespace,
				Organization: bucketDetail.Organization,
			}
			logger.V(1).Info("dividing by billing parts", "billingParts", billingParts)
			usagePart := float64(*bucketUsage.Size) / float64(billingParts)
			adjustedSize, err := adjustStorageSizeUnit(usagePart)
			if err != nil {
				logger.Error(err, "cannot adjust bucket size")
			}
			aggregatedBucket.Value += adjustedSize
			aggregatedBuckets[key] = aggregatedBucket
		} else {
			logger.Info("Could not find any bucket on exoscale", "bucket", bucketDetail.BucketName)
		}
	}
	return aggregatedBuckets
}

func (o *ObjectStorage) fetchManagedBucketsAndNamespaces(ctx context.Context) ([]BucketDetail, error) {
	logger := log.Logger(ctx)
	logger.Info("Fetching buckets and namespaces from cluster")

	buckets := exoscalev1.BucketList{}
	logger.V(1).Info("Listing buckets from cluster")
	err := o.k8sClient.List(ctx, &buckets)
	if err != nil {
		return nil, fmt.Errorf("cannot list buckets: %w", err)
	}

	logger.V(1).Info("Listing namespaces from cluster")
	namespaces, err := kubernetes.FetchNamespaceWithOrganizationMap(ctx, o.k8sClient, o.orgOverride)
	if err != nil {
		return nil, fmt.Errorf("cannot list namespaces: %w", err)
	}

	return addOrgAndNamespaceToBucket(ctx, buckets, namespaces), nil
}

func addOrgAndNamespaceToBucket(ctx context.Context, buckets exoscalev1.BucketList, namespaces map[string]string) []BucketDetail {
	logger := log.Logger(ctx)
	logger.V(1).Info("Gathering org and namespace from buckets")

	bucketDetails := make([]BucketDetail, 0, 10)
	for _, bucket := range buckets.Items {
		bucketDetail := BucketDetail{
			BucketName: bucket.Spec.ForProvider.BucketName,
		}
		if namespace, exist := bucket.ObjectMeta.Labels[namespaceLabel]; exist {
			organization, ok := namespaces[namespace]
			if !ok {
				// cannot find namespace in namespace list
				logger.Info("Namespace not found in namespace list, skipping...",
					"namespace", namespace,
					"bucket", bucket.Name)
				continue
			}
			bucketDetail.Namespace = namespace
			bucketDetail.Organization = organization
		} else {
			// cannot get namespace from bucket
			logger.Info("Namespace label is missing in bucket, skipping...",
				"label", namespaceLabel,
				"bucket", bucket.Name)
			continue
		}
		logger.V(1).Info("Added namespace and organization to bucket",
			"bucket", bucket.Name,
			"namespace", bucketDetail.Namespace,
			"organization", bucketDetail.Organization)
		bucketDetails = append(bucketDetails, bucketDetail)
	}
	return bucketDetails
}

func adjustStorageSizeUnit(value float64) (float64, error) {
	sosUnit := exofixtures.ObjectStorage.Query.Unit
	if sosUnit == exofixtures.DefaultUnitSos {
		return value / 1024 / 1024 / 1024, nil
	}
	return 0, fmt.Errorf("unknown Query unit %s", sosUnit)
}
