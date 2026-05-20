package main

import (
	"flag"
	"os"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	"github.com/sindef/servicer/internal/controllers"
	"github.com/sindef/servicer/internal/deliveryrepo"
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
	var deliveryRepoURL string
	var deliveryRepoRef string
	var deliveryRepoPath string
	var deliveryRepoWorktree string
	var deliveryRepoAutoCommit bool
	var deliveryRepoAutoPush bool
	var deliveryRepoRemote string
	var deliveryRepoBranch string
	var argoCDNamespace string
	var argoCDProject string
	var enableWebhooks bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.StringVar(&deliveryRoot, "delivery-root", materializer.DefaultRoot, "Path where controller-owned generated delivery artifacts are written.")
	flag.StringVar(&deliveryRepoURL, "delivery-repo-url", "", "Git repository URL that Argo CD should use for generated delivery content.")
	flag.StringVar(&deliveryRepoRef, "delivery-repo-ref", "HEAD", "Git revision Argo CD should track for generated delivery content.")
	flag.StringVar(&deliveryRepoPath, "delivery-repo-path", materializer.DefaultRoot, "Repository-relative root path for generated delivery content.")
	flag.StringVar(&deliveryRepoWorktree, "delivery-repo-worktree", "", "Local Git worktree path where generated delivery content should be published.")
	flag.BoolVar(&deliveryRepoAutoCommit, "delivery-repo-auto-commit", false, "Automatically create Git commits in the configured delivery repo worktree after publishing artifacts.")
	flag.BoolVar(&deliveryRepoAutoPush, "delivery-repo-auto-push", false, "Automatically push committed delivery content to the configured Git remote after publishing artifacts.")
	flag.StringVar(&deliveryRepoRemote, "delivery-repo-remote", "origin", "Git remote that should receive published delivery commits when auto-push is enabled.")
	flag.StringVar(&deliveryRepoBranch, "delivery-repo-branch", "", "Git branch that should receive published delivery commits when auto-push is enabled. Defaults to the current worktree branch.")
	flag.StringVar(&argoCDNamespace, "argocd-namespace", "argocd", "Namespace where Argo CD Application resources are created.")
	flag.StringVar(&argoCDProject, "argocd-project", "default", "Argo CD project used for Servicer-managed Applications.")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", false, "Enable admission webhooks for Servicer APIs.")

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
		adapters.NewK8ssandraAdapter(),
		adapters.NewYugabyteAdapter(),
		adapters.NewKubeVirtAdapter(),
		adapters.NewArgoApplicationAdapter(),
	)
	if err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to build adapter registry")
		os.Exit(1)
	}

	if err := (&controllers.TenantReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create tenant controller")
		os.Exit(1)
	}
	if err := (&controllers.PolicyReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create policy controller")
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
	if err := (&controllers.NamespaceClaimReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create namespace claim controller")
		os.Exit(1)
	}
	if err := (&controllers.ServiceBindingReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create service binding controller")
		os.Exit(1)
	}
	if err := (&controllers.VirtualMachineClaimReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create virtual machine claim controller")
		os.Exit(1)
	}
	if err := (&controllers.ServiceInstanceReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Adapters: adapterRegistry, Materializer: materializer.New(deliveryRoot), Publisher: deliveryrepo.New(deliveryRepoWorktree, deliveryRepoPath, deliveryRepoAutoCommit, deliveryRepoAutoPush, deliveryRepoRemote, deliveryRepoBranch), Recorder: mgr.GetEventRecorderFor("servicer"), ArgoCDNamespace: argoCDNamespace, ArgoCDProject: argoCDProject, DeliveryRepoURL: deliveryRepoURL, DeliveryRepoRef: deliveryRepoRef, DeliveryRepoPath: deliveryRepoPath}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create service instance controller")
		os.Exit(1)
	}
	if err := (&controllers.ActionRequestReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Adapters: adapterRegistry}).SetupWithManager(mgr); err != nil {
		ctrl.Log.WithName("setup").Error(err, "unable to create action request controller")
		os.Exit(1)
	}
	if enableWebhooks {
		if err := (&platformv1alpha1.ServiceInstance{}).SetupWebhookWithManager(mgr); err != nil {
			ctrl.Log.WithName("setup").Error(err, "unable to register service instance webhook")
			os.Exit(1)
		}
		if err := (&platformv1alpha1.NamespaceClaim{}).SetupWebhookWithManager(mgr); err != nil {
			ctrl.Log.WithName("setup").Error(err, "unable to register namespace claim webhook")
			os.Exit(1)
		}
		if err := (&platformv1alpha1.ServiceBinding{}).SetupWebhookWithManager(mgr); err != nil {
			ctrl.Log.WithName("setup").Error(err, "unable to register service binding webhook")
			os.Exit(1)
		}
		if err := (&platformv1alpha1.VirtualMachineClaim{}).SetupWebhookWithManager(mgr); err != nil {
			ctrl.Log.WithName("setup").Error(err, "unable to register virtual machine claim webhook")
			os.Exit(1)
		}
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
