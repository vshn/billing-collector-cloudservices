package exoscale

import (
	"context"
	"fmt"
	"time"

	egoscale "github.com/exoscale/egoscale/v3"
	"github.com/vshn/billing-collector-cloudservices/pkg/controlAPI"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/odoo"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

const productIdPrefix = "appcat-exoscale"

var (
	groupVersionKinds = map[string]schema.GroupVersionKind{
		"pg": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "PostgreSQLList",
		},
		"mysql": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "MySQLList",
		},
		"opensearch": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "OpenSearchList",
		},
		"redis": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "RedisList",
		},
		"kafka": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "KafkaList",
		},
	}

	dbaasTypes = map[string]string{
		"pg":         "PostgreSQL",
		"mysql":      "MySQL",
		"opensearch": "OpenSearch",
		"redis":      "Redis",
		"kafka":      "Kafka",
	}
)

// Detail a helper structure for intermediate operations
type Detail struct {
	Organization, DBName, Namespace, Plan, Zone, Kind string
}

// DBaaS provides DBaaS Odoo info and required clients
type DBaaS struct {
	exoscaleClient   *egoscale.Client
	k8sClient        k8s.Client
	controlApiClient k8s.Client
	salesOrder       string
	clusterId        string
	cloudZone        string
	collectInterval  int
	uomMapping       map[string]string
}

// NewDBaaS creates a Service with the initial setup
func NewDBaaS(exoscaleClient *egoscale.Client, k8sClient k8s.Client, controlApiClient k8s.Client, collectInterval int, salesOrder, clusterId string, cloudZone string, uomMapping map[string]string) (*DBaaS, error) {
	return &DBaaS{
		exoscaleClient:   exoscaleClient,
		k8sClient:        k8sClient,
		controlApiClient: controlApiClient,
		salesOrder:       salesOrder,
		clusterId:        clusterId,
		cloudZone:        cloudZone,
		collectInterval:  collectInterval,
		uomMapping:       uomMapping,
	}, nil
}

func (ds *DBaaS) GetMetrics(ctx context.Context) ([]odoo.OdooMeteredBillingRecord, error) {
	detail, err := ds.fetchManagedDBaaSAndNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchManagedDBaaSAndNamespaces: %w", err)
	}

	usage, err := ds.fetchDBaaSUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchDBaaSUsage: %w", err)
	}

	return ds.AggregateDBaaS(ctx, usage, detail)
}

// fetchManagedDBaaSAndNamespaces fetches instances and namespaces from kubernetes cluster
func (ds *DBaaS) fetchManagedDBaaSAndNamespaces(ctx context.Context) ([]Detail, error) {
	logger := log.Logger(ctx)

	logger.V(1).Info("Listing namespaces from cluster")
	namespaces, err := kubernetes.FetchNamespaceWithOrganizationMap(ctx, ds.k8sClient)
	if err != nil {
		return nil, fmt.Errorf("cannot list namespaces: %w", err)
	}

	var dbaasDetails []Detail
	for _, gvk := range groupVersionKinds {
		metaList := &metav1.PartialObjectMetadataList{}
		metaList.SetGroupVersionKind(gvk)
		err := ds.k8sClient.List(ctx, metaList)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("cannot list managed resource kind %s from cluster: %w", gvk.Kind, err)
		}

		for _, item := range metaList.Items {
			dbaasDetail := findDBaaSDetailInNamespacesMap(ctx, item, gvk, namespaces)
			if dbaasDetail == nil {
				continue
			}
			dbaasDetails = append(dbaasDetails, *dbaasDetail)
		}
	}

	return dbaasDetails, nil
}

func findDBaaSDetailInNamespacesMap(ctx context.Context, resource metav1.PartialObjectMetadata, gvk schema.GroupVersionKind, namespaces map[string]string) *Detail {
	logger := log.Logger(ctx).WithValues("dbaas", resource.GetName())

	namespace, exist := resource.GetLabels()[namespaceLabel]
	if !exist {
		// cannot get namespace from DBaaS
		logger.Info("Namespace label is missing in DBaaS, skipping...", "label", namespaceLabel)
		return nil
	}

	organization, ok := namespaces[namespace]
	if !ok {
		// cannot find namespace in namespace list
		logger.Info("Namespace not found in namespace list, skipping...", "namespace", namespace)
		return nil
	}

	dbaasDetail := Detail{
		DBName:       resource.GetName(),
		Kind:         gvk.Kind,
		Namespace:    namespace,
		Organization: organization,
		Zone:         resource.GetAnnotations()["appcat.vshn.io/cloudzone"],
	}

	logger.V(1).Info("Added namespace and organization to DBaaS", "namespace", dbaasDetail.Namespace, "organization", dbaasDetail.Organization)
	return &dbaasDetail
}

// fetchDBaaSUsage gets DBaaS service usage from Exoscale
func (ds *DBaaS) fetchDBaaSUsage(ctx context.Context) ([]egoscale.DBAASServiceCommon, error) {
	logger := log.Logger(ctx)
	logger.Info("Fetching DBaaS usage from Exoscale")

	var databaseServices []egoscale.DBAASServiceCommon
	for _, endpoint := range Endpoints {
		databaseServicesByZone, err := ds.exoscaleClient.WithEndpoint(endpoint).ListDBAASServices(ctx)
		if err != nil {
			logger.V(1).Error(err, "Cannot get exoscale database services on endpoint", "endpoint", endpoint)
			return nil, err
		}
		databaseServices = append(databaseServices, databaseServicesByZone.DBAASServices...)
	}
	return databaseServices, nil
}

// AggregateDBaaS aggregates DBaaS services by namespaces and plan
func (ds *DBaaS) AggregateDBaaS(ctx context.Context, exoscaleDBaaS []egoscale.DBAASServiceCommon, dbaasDetails []Detail) ([]odoo.OdooMeteredBillingRecord, error) {
	logger := log.Logger(ctx)
	logger.Info("Aggregating DBaaS instances by namespace and plan")

	// The DBaaS names are unique across DB types in an Exoscale organization.
	dbaasServiceUsageMap := make(map[string]egoscale.DBAASServiceCommon, len(exoscaleDBaaS))
	for _, usage := range exoscaleDBaaS {
		dbaasServiceUsageMap[string(usage.Name)] = usage
	}

	location, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		return nil, fmt.Errorf("load loaction: %w", err)
	}

	now := time.Now().In(location)
	billingDateStart := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location()).In(time.UTC)
	billingDateEnd := time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location()).In(time.UTC)

	records := make([]odoo.OdooMeteredBillingRecord, 0)
	for _, dbaasDetail := range dbaasDetails {
		logger.V(1).Info("Checking DBaaS", "instance", dbaasDetail.DBName)

		dbaasUsage, exists := dbaasServiceUsageMap[dbaasDetail.DBName]
		if exists && dbaasDetail.Kind == groupVersionKinds[string(dbaasUsage.Type)].Kind {
			logger.V(1).Info("Found exoscale dbaas usage", "instance", dbaasUsage.Name, "instance created", dbaasUsage.CreatedAT)

			itemGroup := fmt.Sprintf("APPUiO Managed - Cluster: %s / Namespace: %s", ds.clusterId, dbaasDetail.Namespace)
			instanceId := fmt.Sprintf("%s/%s", dbaasDetail.Zone, dbaasDetail.DBName)
			salesOrder := ds.salesOrder
			if salesOrder == "" {
				itemGroup = fmt.Sprintf("APPUiO Cloud - Zone: %s / Namespace: %s", ds.cloudZone, dbaasDetail.Namespace)
				salesOrder, err = controlAPI.GetSalesOrder(ctx, ds.controlApiClient, dbaasDetail.Organization)
				if err != nil {
					logger.Error(err, "Unable to sync DBaaS, cannot get salesOrder", "namespace", dbaasDetail.Namespace)
					continue
				}
			}

			o := odoo.OdooMeteredBillingRecord{
				ProductID:            productIdPrefix + fmt.Sprintf("-v2-%s-%s", string(dbaasUsage.Type), dbaasUsage.Plan),
				InstanceID:           instanceId,
				ItemDescription:      dbaasDetail.DBName,
				ItemGroupDescription: itemGroup,
				SalesOrder:           salesOrder,
				UnitID:               ds.uomMapping[odoo.InstanceHour],
				ConsumedUnits:        1,
				TimeRange: odoo.TimeRange{
					From: billingDateStart,
					To:   billingDateEnd,
				},
			}

			records = append(records, o)

		} else {
			logger.Info("Could not find any DBaaS on exoscale", "instance", dbaasDetail.DBName)
		}
	}

	return records, nil
}

func CheckDBaaSUOMExistence(mapping map[string]string) error {
	if mapping[odoo.InstanceHour] == "" {
		return fmt.Errorf("missing UOM mapping %s", odoo.InstanceHour)
	}
	return nil
}
