package k8sutil

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClientOptions struct {
	Kubeconfig *string
	RestConfig *rest.Config
	Scheme     *runtime.Scheme

	QPS   float32
	Burst int
}

func NewK8sClient(options ClientOptions) (client.WithWatch, error) {
	crOpts := client.Options{
		Scheme: options.Scheme,
	}
	restConfig, err := NewRestConfig(options)
	if err != nil {
		return nil, err
	}
	return client.NewWithWatch(restConfig, crOpts)
}

func NewRestConfig(options ClientOptions) (*rest.Config, error) {
	var restConfig *rest.Config
	switch {
	case options.Kubeconfig != nil:
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		rules.ExplicitPath = *options.Kubeconfig
		apiConfig, err := rules.Load()
		if err != nil {
			return nil, err
		}
		restConfig, err = clientcmd.NewDefaultClientConfig(
			*apiConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, err
		}
	case options.RestConfig != nil:
		restConfig = options.RestConfig
	default:
		var err error
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		restConfig.QPS = options.QPS
		restConfig.Burst = options.Burst
	}
	return restConfig, nil
}
