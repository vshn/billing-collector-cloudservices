package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/suite"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/prom"
	"gopkg.in/dnaeon/go-vcr.v3/cassette"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

type Suite struct {
	suite.Suite

	NS      string
	Client  client.Client
	Config  *rest.Config
	Env     *envtest.Environment
	Logger  logr.Logger
	Context context.Context
	cancel  context.CancelFunc
	Scheme  *runtime.Scheme
}

// SetupSuite is used for setting up the testsuite before all tests are run. If you need to override it, make sure to call `SetupEnv()` first.
func (ts *Suite) SetupSuite() {
	ts.SetupEnv(nil, "")
}

func (ts *Suite) SetupEnv(crdPaths []string, bindString string) {
	ts.T().Helper()

	assert := ts.Assert()

	logger, err := log.NewLogger("integrationtest", time.Now().String(), 1, "console")
	assert.NoError(err, "cannot initialize logger")

	ts.Context = log.NewLoggingContext(context.Background(), logger)
	ts.Logger = logger
	ts.Context, ts.cancel = context.WithCancel(ts.Context)

	envtestAssets, ok := os.LookupEnv("KUBEBUILDER_ASSETS")
	if !ok {
		ts.FailNow("The environment variable KUBEBUILDER_ASSETS is undefined. Configure your IDE to set this variable when running the integration test.")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     crdPaths,
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: envtestAssets,
	}

	config, err := testEnv.Start()
	assert.NoError(err)
	assert.NotNil(config)

	ts.Scheme = runtime.NewScheme()
	ts.RegisterScheme(corev1.SchemeBuilder.AddToScheme)

	k8sClient, err := client.New(config, client.Options{
		Scheme: ts.Scheme,
	})
	assert.NoError(err)
	assert.NotNil(k8sClient)

	ts.Env = testEnv
	ts.Config = config
	ts.Client = k8sClient

	go func() {
		assert.NoError(ts.exportMetrics(bindString), "error exportig the metrics")
	}()
}

func (ts *Suite) exportMetrics(bindString string) error {
	return prom.ServeMetrics(ts.Context, bindString)
}

// RegisterScheme passes the current scheme to the given SchemeBuilder func.
func (ts *Suite) RegisterScheme(addToScheme func(s *runtime.Scheme) error) {
	ts.T().Helper()
	ts.Assert().NoError(addToScheme(ts.Scheme))
}

// TearDownSuite implements suite.TearDownAllSuite.
// It is used to shut down the local envtest environment.
func (ts *Suite) TearDownSuite() {
	ts.Logger.Info("starting tear down")
	ts.cancel()
	err := ts.Env.Stop()
	ts.Assert().NoErrorf(err, "error while stopping test environment")
	ts.Logger.Info("test environment stopped")
}

// EnsureResources ensures that the given resources are existing in the suite. Each error will fail the test.
func (ts *Suite) EnsureResources(resources ...client.Object) {
	ts.T().Helper()

	for _, resource := range resources {
		ts.T().Logf("creating resource '%s/%s'", resource.GetNamespace(), resource.GetName())
		ts.Assert().NoError(ts.Client.Create(ts.Context, resource))
	}
}

// EnsureNS creates a new Namespace, validating the name in accordance with k8s validation rules.
func (ts *Suite) EnsureNS(name string, labels map[string]string) {
	ts.T().Helper()

	ts.Assert().Emptyf(validation.IsDNS1123Label(name), "'%s' does not appear to be a valid name for a namespace", name)
	ts.EnsureResources(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	})
}

func RequestRecorder(t *testing.T, path string) (*http.Client, func(), error) {
	t.Helper()

	r, err := recorder.NewWithOptions(&recorder.Options{
		CassetteName:       path,
		Mode:               recorder.ModeRecordOnce,
		RealTransport:      nil,
		SkipRequestLatency: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("recorder: %w", err)
	}
	cancel := func() {
		if err := r.Stop(); err != nil {
			t.Logf("recorder stop: %v", err)
		}
	}

	r.AddHook(func(i *cassette.Interaction) error {
		// ensure API token is not stored in recorded response
		delete(i.Request.Headers, "Authorization")
		return nil
	}, recorder.AfterCaptureHook)

	return r.GetDefaultClient(), cancel, nil
}
