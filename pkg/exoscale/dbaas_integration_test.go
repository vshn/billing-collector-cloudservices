//go:build integration

package exoscale

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/exoscale/egoscale/v2/oapi"
	"github.com/stretchr/testify/suite"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/prom"
	"github.com/vshn/billing-collector-cloudservices/pkg/test"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const dbaasBind = ":9124"

type DBaaSTestSuite struct {
	test.Suite
	billingDate time.Time
}

func (ts *DBaaSTestSuite) SetupSuite() {
	exoscaleCRDPaths := os.Getenv("EXOSCALE_CRDS_PATH")
	ts.Require().NotZero(exoscaleCRDPaths, "missing env variable EXOSCALE_CRDS_PATH")

	ts.SetupEnv([]string{exoscaleCRDPaths}, dbaasBind)

	ts.RegisterScheme(exoscalev1.SchemeBuilder.AddToScheme)
}

func (ts *DBaaSTestSuite) TestMetrics() {
	assert := ts.Assert()
	ctx := ts.Context

	ds, cancel := ts.setupDBaaS()
	defer cancel()

	type testcase struct {
		gvk    schema.GroupVersionKind
		ns     string
		plan   string
		dbType oapi.DbaasServiceTypeName
	}

	nsTenantMap := map[string]string{
		"example-project": "example-company",
		"next-big-thing":  "big-corporation",
	}
	for ns, tenant := range nsTenantMap {
		ts.EnsureNS(ns, map[string]string{kubernetes.OrganizationLabel: tenant})
	}

	tests := make(map[string]testcase)
	for key, gvk := range groupVersionKinds {
		plan := "hobbyist-2"
		// kafka has no hobbyist plan
		if key == "kafka" {
			plan = "startup-2"
		}
		tests[key+"-example-project"] = testcase{
			gvk:    gvk,
			ns:     "example-project",
			plan:   plan,
			dbType: oapi.DbaasServiceTypeName(key),
		}
	}

	tests["pg-expensive-example-project"] = testcase{
		gvk:    groupVersionKinds["pg"],
		ns:     "example-project",
		plan:   "premium-225",
		dbType: "pg",
	}

	tests["pg-next-big-thing"] = testcase{
		gvk:    groupVersionKinds["pg"],
		ns:     "next-big-thing",
		plan:   "business-225",
		dbType: "pg",
	}

	type expectation struct {
		value float64
		tc    testcase
	}
	expectedQuantities := make(map[Key]expectation, 0)

	for name, tc := range tests {
		key := NewKey(tc.ns, tc.plan, string(tc.dbType))
		if _, ok := expectedQuantities[key]; !ok {
			expectedQuantities[key] = expectation{
				value: 0,
				tc:    tc,
			}
		}
		expectedQuantities[key] = expectation{
			value: expectedQuantities[key].value + 1,
			tc:    tc,
		}

		obj := &unstructured.Unstructured{}
		obj.SetUnstructuredContent(map[string]interface{}{
			"apiVersion": tc.gvk.GroupVersion().String(),
			// a bit ugly, but I wanted to avoid adding more than necessary code
			"kind": strings.Replace(tc.gvk.Kind, "List", "", 1),
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]string{
					namespaceLabel: tc.ns,
				},
			},
			"spec": map[string]interface{}{
				"forProvider": map[string]interface{}{
					"zone": "ch-gva-2",
				},
			},
		})
		ts.EnsureResources(obj)
	}

	assertMetrics := []test.PromMetric{
		{
			Product:  "appcat_opensearch:exoscale:example-company:example-project:hobbyist-2",
			Category: "exoscale:example-project",
			Value:    1,
		},
		{
			Product:  "appcat_postgres:exoscale:example-company:example-project:premium-225",
			Category: "exoscale:example-project",
			Value:    1,
		},
		{
			Product:  "appcat_postgres:exoscale:big-corporation:next-big-thing:business-225",
			Category: "exoscale:next-big-thing",
			Value:    1,
		},
		{
			Product:  "appcat_mysql:exoscale:example-company:example-project:hobbyist-2",
			Category: "exoscale:example-project",
			Value:    1,
		},
		{
			Product:  "appcat_opensearch:exoscale:example-company:example-project:hobbyist-2",
			Category: "exoscale:example-project",
			Value:    1,
		},
		{
			Product:  "appcat_redis:exoscale:example-company:example-project:hobbyist-2",
			Category: "exoscale:example-project",
			Value:    1,
		},
		{
			Product:  "appcat_kafka:exoscale:example-company:example-project:startup-2",
			Category: "exoscale:example-project",
			Value:    1,
		},
	}

	metrics, err := ds.Accumulate(ctx)
	assert.NoError(err, "cannot accumulate dbaas")

	assert.NoError(Export(metrics))
	assert.NoError(test.AssertPromMetrics(assert, assertMetrics, dbaasBind), "cannot assert prom metrics")
	prom.ResetAppCatMetric()

}

type dbaasSource struct {
	dbType    string
	tenant    string
	namespace string
	plan      string
}

func (ts *DBaaSTestSuite) ensureBuckets(nameNsMap map[string]string) {
	resources := make([]client.Object, 0)
	for name, ns := range nameNsMap {
		resources = append(resources, &exoscalev1.Bucket{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{namespaceLabel: ns},
			},
			Spec: exoscalev1.BucketSpec{
				ForProvider: exoscalev1.BucketParameters{BucketName: name},
			},
		})
	}
	ts.EnsureResources(resources...)
}

func (ts *DBaaSTestSuite) setupDBaaS() (*DBaaS, func()) {
	exoClient, cancel, err := newEgoscaleClient(ts.T())
	ts.Assert().NoError(err)

	ds, err := NewDBaaS(exoClient, ts.Client, "")
	ts.Assert().NoError(err)
	return ds, cancel
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestDBaaSTestSuite(t *testing.T) {
	suite.Run(t, new(DBaaSTestSuite))
}
