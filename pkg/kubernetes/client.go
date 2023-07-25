package kubernetes

import (
	"context"
	"fmt"

	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	cloudscaleapis "github.com/vshn/provider-cloudscale/apis"
	exoapis "github.com/vshn/provider-exoscale/apis"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// OrganizationLabel represents the label used for organization when fetching the metrics
	OrganizationLabel = "appuio.io/organization"
)

// NewClient creates a k8s client from the server url and token url
// If kubeconfig (path to it) is supplied, that takes precedence. Its use is mainly for local development
// since local clusters usually don't have a valid certificate.
func NewClient(kubeconfig string) (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("core scheme: %w", err)
	}
	if err := exoapis.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("exoscale scheme: %w", err)
	}
	if err := cloudscaleapis.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("cloudscale scheme: %w", err)
	}

	c, err := client.New(ctrl.GetConfigOrDie(), client.Options{
		Scheme: scheme,
	})

	if err != nil {
		return nil, fmt.Errorf("cannot initialize k8s client: %w", err)
	}
	return c, nil
}

func restConfig(kubeconfig string) (*rest.Config, error) {
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

func FetchNamespaceWithOrganizationMap(ctx context.Context, k8sClient client.Client, orgOverride string) (map[string]string, error) {
	logger := log.Logger(ctx)

	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "NamespaceList",
	}
	list := &metav1.PartialObjectMetadataList{}
	list.SetGroupVersionKind(gvk)

	err := k8sClient.List(ctx, list)
	if err != nil {
		return nil, fmt.Errorf("cannot get namespace list: %w", err)
	}

	namespaces := map[string]string{}
	for _, ns := range list.Items {
		org := orgOverride
		if org == "" {
			orgLabel, ok := ns.GetLabels()[OrganizationLabel]
			if !ok {
				logger.Info("Organization label not found in namespace", "namespace", ns.GetName())
				continue
			}
			org = orgLabel
		}

		namespaces[ns.GetName()] = org
	}
	return namespaces, nil
}
