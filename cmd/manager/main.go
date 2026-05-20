package main

import (
	"flag"
	"os"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"github.com/sindef/servicer/internal/controllers"
	"github.com/sindef/servicer/internal/materializer"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(platformv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	var deliveryRoot string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.StringVar(&deliveryRoot, "delivery-root", materializer.DefaultRoot, "Path where controller-owned generated delivery artifacts are written.")

	zapOptions := zap.Options{Development: true}
	zapOptions.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOptions)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "manager.platform.servicer.io",
	})
	if err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to start manager")
		os.Exit(1)
	}

	adapterRegistry, err := adapters.NewRegistry(
		adapters.NewNamespaceAdapter(),
		adapters.NewCNPGAdapter(),
		adapters.NewMySQLAdapter(),
		adapters.NewValkeyAdapter(),
		adapters.NewNATSAdapter(),
		adapters.NewYugabyteAdapter(),
	)
	if err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to build adapter registry")
		os.Exit(1)
	}

	if err := (&controllers.TenantReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create tenant controller")
		os.Exit(1)
	}
	if err := (&controllers.ClusterTargetReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create cluster target controller")
		os.Exit(1)
	}
	if err := (&controllers.ServiceClassReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Adapters: adapterRegistry}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create service class controller")
		os.Exit(1)
	}
	if err := (&controllers.ServicePlanReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create service plan controller")
		os.Exit(1)
	}
	if err := (&controllers.ProjectReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create project controller")
		os.Exit(1)
	}
	if err := (&controllers.ServiceInstanceReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Adapters: adapterRegistry, Materializer: materializer.New(deliveryRoot), Recorder: mgr.GetEventRecorderFor("servicer")}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create service instance controller")
		os.Exit(1)
	}
	if err := (&controllers.ActionRequestReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Adapters: adapterRegistry}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create action request controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	ctrl.Log.WithName("setup").Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.WithName("setup").Error(err, "problem running manager")
		os.Exit(1)
	}
}
