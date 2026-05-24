package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/bff"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(platformv1alpha1.AddToScheme(scheme))
}

func main() {
	var listenAddr string
	var tlsListenAddr string
	var metricsListenAddr string
	flag.StringVar(&listenAddr, "listen", ":8090", "Address for the Servicer BFF HTTP server.")
	flag.StringVar(&tlsListenAddr, "tls-listen", ":8443", "Address for the Servicer BFF HTTPS Kubernetes proxy listener. Leave empty to disable.")
	flag.StringVar(&metricsListenAddr, "metrics-listen", ":9090", "Address for the Servicer BFF metrics server. Leave empty to disable.")

	zapOptions := zap.Options{Development: true}
	zapOptions.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOptions)))

	restConfig := ctrl.GetConfigOrDie()
	kubeClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		ctrl.Log.WithName("bff").Error(err, "unable to create Kubernetes client")
		os.Exit(1)
	}

	server := bff.NewServerWithConfig(kubeClient, restConfig)
	errs := make(chan error, 2)
	if listenAddr != "" {
		go func() {
			httpServer := productionHTTPServer(listenAddr, server.Handler())
			ctrl.Log.WithName("bff").Info("starting BFF", "listen", listenAddr)
			errs <- httpServer.ListenAndServe()
		}()
	}
	if tlsListenAddr != "" {
		tlsConfig, err := selfSignedLocalhostTLSConfig()
		if err != nil {
			ctrl.Log.WithName("bff").Error(err, "unable to create local TLS config")
			os.Exit(1)
		}
		go func() {
			httpsServer := &http.Server{
				Addr:              tlsListenAddr,
				Handler:           server.Handler(),
				TLSConfig:         tlsConfig,
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       15 * time.Second,
				WriteTimeout:      30 * time.Second,
				IdleTimeout:       60 * time.Second,
			}
			ctrl.Log.WithName("bff").Info("starting BFF HTTPS proxy", "listen", tlsListenAddr)
			listener, err := net.Listen("tcp", tlsListenAddr)
			if err != nil {
				errs <- err
				return
			}
			errs <- httpsServer.ServeTLS(listener, "", "")
		}()
	}
	if listenAddr == "" && tlsListenAddr == "" {
		ctrl.Log.WithName("bff").Error(nil, "at least one of --listen or --tls-listen must be set")
		os.Exit(1)
	}
	if metricsListenAddr != "" {
		go func() {
			metricsServer := productionHTTPServer(metricsListenAddr, server.MetricsHandler())
			ctrl.Log.WithName("bff").Info("starting BFF metrics", "listen", metricsListenAddr)
			errs <- metricsServer.ListenAndServe()
		}()
	}
	if err := <-errs; err != nil {
		ctrl.Log.WithName("bff").Error(err, "BFF server stopped")
		os.Exit(1)
	}
}

func productionHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func selfSignedLocalhostTLSConfig() (*tls.Config, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: "servicer-local-proxy",
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}, nil
}
