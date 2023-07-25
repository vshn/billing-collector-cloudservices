//go:build integration

package cloudscale

import (
	"os"
	"testing"
	"time"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v2"
	"github.com/stretchr/testify/suite"
	"github.com/vshn/billing-collector-cloudservices/pkg/prom"
	"github.com/vshn/billing-collector-cloudservices/pkg/test"
	cloudscalev1 "github.com/vshn/provider-cloudscale/apis/cloudscale/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const objectStorageBind = ":9123"

type ObjectStorageTestSuite struct {
	test.Suite
	billingDate time.Time
}

func (ts *ObjectStorageTestSuite) SetupSuite() {
	cloudscaleCRDsPath := os.Getenv("CLOUDSCALE_CRDS_PATH")
	ts.Require().NotZero(cloudscaleCRDsPath, "missing env variable CLOUDSCALE_CRDS_PATH")

	ts.SetupEnv([]string{cloudscaleCRDsPath}, objectStorageBind)

	ts.RegisterScheme(cloudscalev1.SchemeBuilder.AddToScheme)
}

// TestMetrics sets up a couple of buckets and associated namespaces with organizations set.
// The cloudscale client is set up with an HTTP replay recorder (go-vcr) which looks into testdata/ for recorded
// HTTP responses.
// For simplicity reasons, the recorder only uses URL and method for matching recorded responses. The upside
// of this is it doesn't matter when we execute the tests since the date used to fetch metrics doesn't matter for matching.
// Downside of course is it doesn't do any validation related to the date matching but that's not the main thing to test here.
func (ts *ObjectStorageTestSuite) TestMetrics() {
	assert := ts.Assert()
	ctx := ts.Context

	o, cancel := ts.setupObjectStorage()
	defer cancel()

	assertMetrics := []test.PromMetric{
		{
			Category: "cloudscale:example-project",
			Product:  "appcat_object-storage-requests:cloudscale:example-company:example-project",
			Value:    100.001,
		},
		{
			Category: "cloudscale:example-project",
			Product:  "appcat_object-storage-storage:cloudscale:example-company:example-project",
			Value:    1000.000004096,
		},
		{
			Category: "cloudscale:example-project",
			Product:  "appcat_object-storage-traffic-out:cloudscale:example-company:example-project",
			Value:    50,
		},
		{
			Category: "cloudscale:next-big-thing",
			Product:  "appcat_object-storage-requests:cloudscale:big-corporation:next-big-thing",
			Value:    0.001,
		},
		{
			Category: "cloudscale:next-big-thing",
			Product:  "appcat_object-storage-storage:cloudscale:big-corporation:next-big-thing",
			Value:    0,
		},
		{
			Category: "cloudscale:next-big-thing",
			Product:  "appcat_object-storage-traffic-out:cloudscale:big-corporation:next-big-thing",
			Value:    0,
		},
	}
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

	createdNs := make(map[string]bool)
	for _, ns := range nameNsMap {
		if _, ok := createdNs[ns]; !ok {
			ts.EnsureNS(ns, map[string]string{organizationLabel: nsTenantMap[ns]})
			createdNs[ns] = true
		}
	}

	testDate := time.Date(2023, 1, 11, 0, 0, 0, 0, time.Local)
	metrics, err := o.Accumulate(ctx, testDate)
	assert.NoError(err)

	// This test doesn't divide the values, it tests with 1 hour remaining for the day
	assert.NoError(Export(metrics, 23))
	assert.NoError(test.AssertPromMetrics(assert, assertMetrics, objectStorageBind), "cannot assert prom metrics")
	prom.ResetAppCatMetric()

	// This test simulates exporting the metrics from 06:00 for the rest of the day
	// For that we'll have to divide the values by 18 to match up.
	for i := range assertMetrics {
		assertMetrics[i].Value = assertMetrics[i].Value / float64(18)
	}
	assert.NoError(Export(metrics, 6))
	assert.NoError(test.AssertPromMetrics(assert, assertMetrics, objectStorageBind), "cannot assert prom metrics")
	prom.ResetAppCatMetric()
}

func (ts *ObjectStorageTestSuite) ensureBuckets(nameNsMap map[string]string) {
	resources := make([]client.Object, 0)
	for name, ns := range nameNsMap {
		resources = append(resources, &cloudscalev1.Bucket{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{namespaceLabel: ns},
			},
			Spec: cloudscalev1.BucketSpec{
				ForProvider: cloudscalev1.BucketParameters{BucketName: name},
			},
		})
	}
	ts.EnsureResources(resources...)
}

func (ts *ObjectStorageTestSuite) setupObjectStorage() (*ObjectStorage, func()) {
	assert := ts.Assert()
	httpClient, cancel, err := test.RequestRecorder(ts.T(), "testdata/cloudscale/"+ts.T().Name())
	assert.NoError(err)

	c := cloudscale.NewClient(httpClient)
	// required to be set when recording new response.
	if apiToken := os.Getenv("CLOUDSCALE_API_TOKEN"); apiToken != "" {
		c.AuthToken = apiToken
		ts.T().Log("API token set")
	} else {
		ts.T().Log("no API token provided")
	}

	o, err := NewObjectStorage(c, ts.Client, "")
	assert.NoError(err)

	return o, cancel
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestObjectStorageTestSuite(t *testing.T) {
	suite.Run(t, new(ObjectStorageTestSuite))
}
