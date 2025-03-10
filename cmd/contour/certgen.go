// Copyright Project Contour Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/projectcontour/contour/internal/certgen"
	"github.com/projectcontour/contour/internal/k8s"
	"github.com/projectcontour/contour/pkg/certs"
	"github.com/sirupsen/logrus"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// registercertgen registers the certgen subcommand and flags
// with the Application provided.
func registerCertGen(app *kingpin.Application) (*kingpin.CmdClause, *certgenConfig) {
	var certgenConfig certgenConfig
	certgenApp := app.Command("certgen", "Generate new TLS certs for bootstrapping gRPC over TLS.")

	certgenApp.Flag("kube", "Apply the generated certs directly to the current Kubernetes cluster.").BoolVar(&certgenConfig.OutputKube)
	certgenApp.Flag("yaml", "Render the generated certs as Kubernetes Secrets in YAML form to the current directory.").BoolVar(&certgenConfig.OutputYAML)
	certgenApp.Flag("pem", "Render the generated certs as individual PEM files to the current directory.").BoolVar(&certgenConfig.OutputPEM)
	certgenApp.Flag("incluster", "Use in cluster configuration.").BoolVar(&certgenConfig.InCluster)
	certgenApp.Flag("kubeconfig", "Path to kubeconfig (if not in running inside a cluster).").Default(filepath.Join(os.Getenv("HOME"), ".kube", "config")).StringVar(&certgenConfig.KubeConfig)
	certgenApp.Flag("namespace", "Kubernetes namespace, used for Kube objects.").Default(certs.DefaultNamespace).Envar("CONTOUR_NAMESPACE").StringVar(&certgenConfig.Namespace)
	// NOTE: --certificate-lifetime can be used to accept Duration string once certificate rotation is supported.
	certgenApp.Flag("certificate-lifetime", "Generated certificate lifetime (in days).").Default(strconv.Itoa(certs.DefaultCertificateLifetime)).UintVar(&certgenConfig.Lifetime)
	certgenApp.Flag("overwrite", "Overwrite existing files or Secrets.").BoolVar(&certgenConfig.Overwrite)
	certgenApp.Flag("secrets-format", "Specify how to format the generated Kubernetes Secrets.").Default("legacy").StringVar(&certgenConfig.Format)
	certgenApp.Flag("secrets-name-prefix", "Specify a prefix to be used as the first part of the generated Kubernetes secrets' names.").StringVar(&certgenConfig.NamePrefix)

	certgenApp.Arg("outputdir", "Directory to write output files into (default \"certs\").").Default("certs").StringVar(&certgenConfig.OutputDir)

	return certgenApp, &certgenConfig
}

// certgenConfig holds the configuration for the certificate generation process.
type certgenConfig struct {

	// KubeConfig is the path to the Kubeconfig file if we're not running in a cluster
	KubeConfig string

	// Incluster means that we should assume we are running in a Kubernetes cluster and work accordingly.
	InCluster bool

	// Namespace is the namespace to put any generated config into for YAML or Kube outputs.
	Namespace string

	// OutputDir stores the directory where any requested files will be output.
	OutputDir string

	// OutputKube means that the certs generated will be output into a Kubernetes cluster as secrets.
	OutputKube bool

	// OutputYAML means that the certs generated will be output into Kubernetes secrets as YAML in the current directory.
	OutputYAML bool

	// OutputPEM means that the certs generated will be output as PEM files in the current directory.
	OutputPEM bool

	// Lifetime is the number of days for which certificates will be valid.
	Lifetime uint

	// Overwrite allows certgen to overwrite any existing files or Kubernetes Secrets.
	Overwrite bool

	// Format specifies how to format the Kubernetes Secrets (must be "legacy" or "compat").
	Format string

	// NamePrefix specifies the prefix to use for the generated Kubernetes secrets' names.
	NamePrefix string
}

// OutputCerts outputs the certs in certs as directed by config.
func OutputCerts(config *certgenConfig, kubeclient *kubernetes.Clientset, certs *certs.Certificates) error {
	secrets := []*corev1.Secret{}
	force := certgen.NoOverwrite
	if config.Overwrite {
		force = certgen.Overwrite
	}

	if config.OutputYAML || config.OutputKube {
		switch config.Format {
		case "legacy":
			secrets = certgen.AsLegacySecrets(config.Namespace, config.NamePrefix, certs)
		case "compact":
			secrets = certgen.AsSecrets(config.Namespace, config.NamePrefix, certs)
		default:
			return fmt.Errorf("unsupported Secrets format %q", config.Format)
		}
	}

	if config.OutputPEM {
		fmt.Printf("Writing certificates to PEM files in %s/\n", config.OutputDir)
		if err := certgen.WriteCertsPEM(config.OutputDir, certs, force); err != nil {
			return fmt.Errorf("failed to write certificates to %q: %w", config.OutputDir, err)
		}
	}

	if config.OutputYAML {
		fmt.Printf("Writing %q format Secrets to YAML files in %s/\n", config.Format, config.OutputDir)
		if err := certgen.WriteSecretsYAML(config.OutputDir, secrets, force); err != nil {
			return fmt.Errorf("failed to write Secrets to %q: %w", config.OutputDir, err)
		}
	}

	if config.OutputKube {
		fmt.Printf("Writing %q format Secrets to namespace %q\n", config.Format, config.Namespace)
		if err := certgen.WriteSecretsKube(kubeclient, secrets, force); err != nil {
			return fmt.Errorf("failed to write certificates to %q: %w", config.Namespace, err)
		}
	}
	return nil
}

func doCertgen(config *certgenConfig, log logrus.FieldLogger) {
	generatedCerts, err := certs.GenerateCerts(
		&certs.Configuration{
			Lifetime:  config.Lifetime,
			Namespace: config.Namespace,
		})
	if err != nil {
		log.WithError(err).Fatal("failed to generate certificates")
	}

	coreClient, err := k8s.NewCoreClient(config.KubeConfig, config.InCluster)
	if err != nil {
		log.WithError(err).Fatalf("failed to create Kubernetes client")
	}

	if oerr := OutputCerts(config, coreClient, generatedCerts); oerr != nil {
		log.WithError(oerr).Fatalf("failed output certificates")
	}

}
