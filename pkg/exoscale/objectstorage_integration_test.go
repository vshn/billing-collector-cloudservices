//go:build integration

package exoscale

import (
	"fmt"
	"os"
	"testing"
	"time"

	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/stretchr/testify/suite"
	"github.com/vshn/billing-collector-cloudservices/pkg/exofixtures"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/prom"
	"github.com/vshn/billing-collector-cloudservices/pkg/test"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const objectStorageBind = ":9125"

type ObjectStorageTestSuite struct {
	test.Suite
	billingDate time.Time
}

func (ts *ObjectStorageTestSuite) SetupSuite() {
	exoscaleCRDPaths := os.Getenv("EXOSCALE_CRDS_PATH")
	ts.Require().NotZero(exoscaleCRDPaths, "missing env variable EXOSCALE_CRDS_PATH")

	ts.SetupEnv([]string{exoscaleCRDPaths}, objectStorageBind)

	ts.RegisterScheme(exoscalev1.SchemeBuilder.AddToScheme)
}

type objectStorageSource struct {
	namespace   string
	tenant      string
	objectType  exofixtures.ObjectType
	billingDate time.Time
}

func (ts *ObjectStorageTestSuite) TestMetrics() {
	assert := ts.Assert()
	ctx := ts.Context

	o, cancel := ts.setupObjectStorage()
	defer cancel()

	nameNsMap := map[string]string{
		"example-project-a": "example-project",
		"example-project-b": "example-project",
		"next-big-thing-a":  "next-big-thing",
	}
	nsTenantMap := map[string]string{
		"example-project": "example-company",
		"next-big-thing":  "big-corporation",
	}
	ts.ensureBuckets(nameNsMap)

	for ns, tenant := range nsTenantMap {
		ts.EnsureNS(ns, map[string]string{kubernetes.OrganizationLabel: tenant})
	}

	assertMetrics := []test.PromMetric{
		{
			Product:  "appcat_object-storage-storage:exoscale:example-company:example-project",
			Category: "exoscale:example-project",
			Value:    932.253897190094,
		},
		{
			Product:  "appcat_object-storage-storage:exoscale:big-corporation:next-big-thing",
			Category: "exoscale:next-big-thing",
			Value:    0,
		},
	}

	// This test doesn't divide the values, it tests with 1 hour remaining for the day
	metrics, err := o.Accumulate(ctx, 23)
	assert.NoError(err, "cannot accumulate exoscale object storage")
	assert.NoError(Export(metrics))
	assert.NoError(test.AssertPromMetrics(assert, assertMetrics, objectStorageBind), "cannot assert prom metrics")
	prom.ResetAppCatMetric()

	// This test simulates exporting the metrics from 06:00 for the rest of the day
	// For that we'll have to divide the values by 18 to match up.
	for i := range assertMetrics {
		assertMetrics[i].Value = assertMetrics[i].Value / float64(18)
	}
	metrics, err = o.Accumulate(ctx, 6)
	assert.NoError(err, "cannot accumulate exoscale object storage")
	assert.NoError(Export(metrics))
	assert.NoError(test.AssertPromMetrics(assert, assertMetrics, objectStorageBind), "cannot assert prom metrics")
	prom.ResetAppCatMetric()
}

func (ts *ObjectStorageTestSuite) ensureBuckets(nameNsMap map[string]string) {
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

func (ts *ObjectStorageTestSuite) setupObjectStorage() (*ObjectStorage, func()) {
	exoClient, cancel, err := newEgoscaleClient(ts.T())
	ts.Assert().NoError(err)

	o, err := NewObjectStorage(exoClient, ts.Client, "")
	ts.Assert().NoError(err)
	return o, cancel
}

func newEgoscaleClient(t *testing.T) (*egoscale.Client, func(), error) {
	httpClient, cancel, err := test.RequestRecorder(t, "testdata/exoscale/"+t.Name())
	if err != nil {
		return nil, nil, fmt.Errorf("request recorder: %w", err)
	}

	apiKey := os.Getenv("EXOSCALE_API_KEY")
	secret := os.Getenv("EXOSCALE_API_SECRET")
	if apiKey != "" && secret != "" {
		t.Log("api key & secret set")
	} else {
		// override empty values since otherwise egoscale complains
		apiKey = "NOTVALID"
		secret = "NOTVALIDSECRET"
		t.Log("api key or secret not set")
	}

	exoClient, err := NewClientWithOptions(apiKey, secret, egoscale.ClientOptWithHTTPClient(httpClient))
	if err != nil {
		return nil, nil, fmt.Errorf("new client: %w", err)
	}
	return exoClient, cancel, nil
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestObjectStorageTestSuite(t *testing.T) {
	suite.Run(t, new(ObjectStorageTestSuite))
}
