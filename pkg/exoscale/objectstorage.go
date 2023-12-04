package exoscale

import (
	"context"
	"fmt"
	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/exoscale/egoscale/v2/oapi"
	apiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/vshn/billing-collector-cloudservices/pkg/exofixtures"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/odoo"
	"github.com/vshn/billing-collector-cloudservices/pkg/prom"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	"time"

	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

const productIdStorage = "appcat-exoscale-object-storage"

// ObjectStorage gathers bucket data from exoscale provider and cluster and saves to the database
type ObjectStorage struct {
	k8sClient      k8s.Client
	exoscaleClient *egoscale.Client
	promClient     apiv1.API
	salesOrderId   string
	clusterId      string
	organization   string
}

// BucketDetail a k8s bucket object with relevant data
type BucketDetail struct {
	Organization, BucketName, Namespace, Zone string
}

// NewObjectStorage creates an ObjectStorage with the initial setup
func NewObjectStorage(exoscaleClient *egoscale.Client, k8sClient k8s.Client, promClient apiv1.API, salesOrderId, clusterId string) (*ObjectStorage, error) {
	return &ObjectStorage{
		k8sClient:      k8sClient,
		exoscaleClient: exoscaleClient,
		promClient:     promClient,
		salesOrderId:   salesOrderId,
		clusterId:      clusterId,
	}, nil
}

func (o *ObjectStorage) GetMetrics(ctx context.Context) ([]odoo.OdooMeteredBillingRecord, error) {
	detail, err := o.fetchManagedBucketsAndNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchManagedBucketsAndNamespaces: %w", err)
	}

	metrics, err := o.getBucketUsage(ctx, detail)
	if err != nil {
		return nil, fmt.Errorf("getBucketUsage: %w", err)
	}
	return metrics, nil
}

// getBucketUsage gets bucket usage from Exoscale and matches them with the bucket from the cluster
// If there are no buckets in Exoscale, the API will return an empty slice
func (o *ObjectStorage) getBucketUsage(ctx context.Context, bucketDetails []BucketDetail) ([]odoo.OdooMeteredBillingRecord, error) {
	logger := log.Logger(ctx)
	logger.Info("Fetching bucket usage from Exoscale")

	resp, err := o.exoscaleClient.ListSosBucketsUsageWithResponse(ctx)
	if err != nil {
		return nil, err
	}

	odooMetrics, err := getOdooMeteredBillingRecords(ctx, o.promClient, *resp.JSON200.SosBucketsUsage, bucketDetails, o.salesOrderId, o.clusterId)
	if err != nil {
		return nil, err
	}
	if len(odooMetrics) == 0 {
		logger.Info("There are no bucket usage to be exported")
		return nil, nil
	}

	return odooMetrics, nil
}

func getOdooMeteredBillingRecords(ctx context.Context, promClient apiv1.API, sosBucketsUsage []oapi.SosBucketUsage, bucketDetails []BucketDetail, salesOrderId, clusterId string) ([]odoo.OdooMeteredBillingRecord, error) {
	logger := log.Logger(ctx)
	logger.Info("Aggregating buckets by namespace")

	sosBucketsUsageMap := make(map[string]oapi.SosBucketUsage, len(sosBucketsUsage))
	for _, usage := range sosBucketsUsage {
		sosBucketsUsageMap[*usage.Name] = usage
	}

	location, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		return nil, fmt.Errorf("load loaction: %w", err)
	}

	now := time.Now().In(location)
	billingDate := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, now.Location()).In(time.UTC)

	aggregatedBuckets := make([]odoo.OdooMeteredBillingRecord, 0)
	for _, bucketDetail := range bucketDetails {
		logger.V(1).Info("Checking bucket", "bucket", bucketDetail.BucketName)

		if bucketUsage, exists := sosBucketsUsageMap[bucketDetail.BucketName]; exists {
			logger.V(1).Info("Found exoscale bucket usage", "bucket", bucketUsage.Name, "bucket size", bucketUsage.Name)
			value, err := adjustStorageSizeUnit(float64(*bucketUsage.Size))
			if err != nil {
				return nil, err
			}

			itemGroup := fmt.Sprintf("APPUiO Managed - Zone: %s / Namespace: %s", clusterId, bucketDetail.Namespace)
			instanceId := fmt.Sprintf("%s/%s", bucketDetail.Zone, bucketDetail.BucketName)
			if salesOrderId == "" {
				itemGroup = fmt.Sprintf("APPUiO Cloud - Zone: %s / Namespace: %s", clusterId, bucketDetail.Namespace)
				salesOrderId, err = prom.GetSalesOrderId(ctx, promClient, bucketDetail.Organization)
				if err != nil {
					logger.Error(err, "unable to sync bucket", "namespace", bucketDetail.Namespace)
					continue
				}
			}

			o := odoo.OdooMeteredBillingRecord{
				ProductID:            productIdStorage,
				InstanceID:           instanceId,
				ItemDescription:      "AppCat Exoscale ObjectStorage",
				ItemGroupDescription: itemGroup,
				SalesOrderID:         salesOrderId,
				UnitID:               exofixtures.DefaultUnitSos,
				ConsumedUnits:        value,
				TimeRange: odoo.TimeRange{
					From: billingDate,
					To:   billingDate.AddDate(0, 0, 1),
				},
			}

			aggregatedBuckets = append(aggregatedBuckets, o)

		} else {
			logger.Info("Could not find any bucket on exoscale", "bucket", bucketDetail.BucketName)
		}
	}
	return aggregatedBuckets, nil
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
	namespaces, err := kubernetes.FetchNamespaceWithOrganizationMap(ctx, o.k8sClient)
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
			Zone:       bucket.Spec.ForProvider.Zone,
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
