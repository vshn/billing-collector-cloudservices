package exoscale

import (
	"context"
	"fmt"
	"strings"

	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/vshn/billing-collector-cloudservices/pkg/exofixtures"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/prom"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

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
)

// Detail a helper structure for intermediate operations
type Detail struct {
	Organization, DBName, Namespace, Plan, Zone, Kind string
}

// DBaaS provides DBaaS Database info and required clients
type DBaaS struct {
	exoscaleClient *egoscale.Client
	k8sClient      k8s.Client
	orgOverride    string
}

// NewDBaaS creates a Service with the initial setup
func NewDBaaS(exoscaleClient *egoscale.Client, k8sClient k8s.Client, orgOverride string) (*DBaaS, error) {
	return &DBaaS{
		exoscaleClient: exoscaleClient,
		k8sClient:      k8sClient,
		orgOverride:    orgOverride,
	}, nil
}

func (ds *DBaaS) Accumulate(ctx context.Context) (map[Key]Aggregated, error) {
	detail, err := ds.fetchManagedDBaaSAndNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchManagedDBaaSAndNamespaces: %w", err)
	}

	usage, err := ds.fetchDBaaSUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchDBaaSUsage: %w", err)
	}

	return aggregateDBaaS(ctx, usage, detail), nil
}

// fetchManagedDBaaSAndNamespaces fetches instances and namespaces from kubernetes cluster
func (ds *DBaaS) fetchManagedDBaaSAndNamespaces(ctx context.Context) ([]Detail, error) {
	logger := log.Logger(ctx)

	logger.V(1).Info("Listing namespaces from cluster")
	namespaces, err := kubernetes.FetchNamespaceWithOrganizationMap(ctx, ds.k8sClient, ds.orgOverride)
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
	}

	logger.V(1).Info("Added namespace and organization to DBaaS", "namespace", dbaasDetail.Namespace, "organization", dbaasDetail.Organization)
	return &dbaasDetail
}

// fetchDBaaSUsage gets DBaaS service usage from Exoscale
func (ds *DBaaS) fetchDBaaSUsage(ctx context.Context) ([]*egoscale.DatabaseService, error) {
	logger := log.Logger(ctx)
	logger.Info("Fetching DBaaS usage from Exoscale")

	var databaseServices []*egoscale.DatabaseService
	for _, zone := range Zones {
		databaseServicesByZone, err := ds.exoscaleClient.ListDatabaseServices(ctx, zone)
		if err != nil {
			logger.V(1).Error(err, "Cannot get exoscale database services on zone", "zone", zone)
			return nil, err
		}
		databaseServices = append(databaseServices, databaseServicesByZone...)
	}
	return databaseServices, nil
}

// aggregateDBaaS aggregates DBaaS services by namespaces and plan
func aggregateDBaaS(ctx context.Context, exoscaleDBaaS []*egoscale.DatabaseService, dbaasDetails []Detail) map[Key]Aggregated {
	logger := log.Logger(ctx)
	logger.Info("Aggregating DBaaS instances by namespace and plan")

	// The DBaaS names are unique across DB types in an Exoscale organization.
	dbaasServiceUsageMap := make(map[string]egoscale.DatabaseService, len(exoscaleDBaaS))
	for _, usage := range exoscaleDBaaS {
		dbaasServiceUsageMap[*usage.Name] = *usage
	}

	aggregatedDBaasS := make(map[Key]Aggregated)
	for _, dbaasDetail := range dbaasDetails {
		logger.V(1).Info("Checking DBaaS", "instance", dbaasDetail.DBName)

		dbaasUsage, exists := dbaasServiceUsageMap[dbaasDetail.DBName]
		if exists && dbaasDetail.Kind == groupVersionKinds[*dbaasUsage.Type].Kind {
			logger.V(1).Info("Found exoscale dbaas usage", "instance", dbaasUsage.Name, "instance created", dbaasUsage.CreatedAt)
			key := NewKey(dbaasDetail.Namespace, *dbaasUsage.Plan, *dbaasUsage.Type)
			aggregated := aggregatedDBaasS[key]
			aggregated.Key = key
			aggregated.Source = &exofixtures.DBaaSSourceString{
				Organization: dbaasDetail.Organization,
				Namespace:    dbaasDetail.Namespace,
				Plan:         *dbaasUsage.Plan,
				Query:        exofixtures.BillingTypes[*dbaasUsage.Type],
			}
			aggregated.Value++
			aggregatedDBaasS[key] = aggregated
		} else {
			logger.Info("Could not find any DBaaS on exoscale", "instance", dbaasDetail.DBName)
		}
	}

	return aggregatedDBaasS
}

func Export(accumulated map[Key]Aggregated) error {

	prom.ResetAppCatMetric()
	for _, val := range accumulated {
		prodType := "dbaas"
		if strings.Contains(val.Source.GetSourceString(), "object") {
			prodType = "objectstorage"
		}
		prom.UpdateAppCatMetric(val.Value, val.Source.GetCategoryString(), val.Source.GetSourceString(), prodType)
	}
	return nil

}
