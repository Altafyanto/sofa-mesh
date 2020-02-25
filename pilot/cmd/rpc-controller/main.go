/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"
	"net/http"
	"time"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"os"
	"strings"

	clientset "istio.io/istio/pkg/rpccontroller/clientset/versioned"
	"istio.io/istio/pkg/rpccontroller/controller"
	informers "istio.io/istio/pkg/rpccontroller/informers/externalversions"
	"istio.io/istio/pkg/signals"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

var (
	masterURL  string
	kubeconfig string

	// for health check
	healthPort int

	// core dns address
	corednsAddress string

	etcdKeyFile   string
	etcdCertFile  string
	etcdCaCertile string
	etcdEndpoints string
)

var (
	loggingOptions = log.DefaultOptions()

	rootCmd = &cobra.Command{
		Use:          "rpc-controller",
		Short:        "Istio rpc controller",
		Long:         "Istio rpc controller.",
		SilenceUsage: true,
	}

	proxyCmd = &cobra.Command{
		Use:   "run",
		Short: "run rpc controller",
		RunE: func(c *cobra.Command, args []string) error {
			if err := log.Configure(loggingOptions); err != nil {
				return err
			}
			log.Infof("Version %s", version.Info.String())

			// start http health check server
			go startHealthCheckHTTPServer(healthPort)

			stopCh := signals.SetupSignalHandler()

			if err := log.Configure(loggingOptions); err != nil {
				return err
			}

			cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
			if err != nil {
				log.Errorf("Error building kubeconfig: %s", err.Error())
				return err
			}

			config := &controller.Config{}
			config.CoreDnsAddress = corednsAddress
			config.EtcdKeyFile = etcdKeyFile
			config.EtcdCertFile = etcdCertFile
			config.EtcdCaCertFile = etcdCaCertile
			config.EtcdEndpoints = strings.Split(etcdEndpoints, ",")

			kubeClient, err := kubernetes.NewForConfig(cfg)
			if err != nil {
				log.Errorf("Error building kubernetes clientset: %s", err.Error())
				return err
			}

			kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)

			watcherClient, err := clientset.NewForConfig(cfg)
			if err != nil {
				log.Errorf("Error building watcher clientset: %s", err.Error())
				return err
			}

			watcherInformerFactory := informers.NewSharedInformerFactory(watcherClient, time.Second*30)

			controller := controller.NewController(kubeClient, watcherClient,
				watcherInformerFactory.Rpccontroller().V1().RpcServices(), config, stopCh)

			go kubeInformerFactory.Start(stopCh)
			go watcherInformerFactory.Start(stopCh)

			if err = controller.Run(2); err != nil {
				log.Errorf("Error running controller: %s", err.Error())
				return err
			}

			return nil
		},
	}
)

func startHealthCheckHTTPServer(port int) {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	log.Infof("Health check HTTP server listening at :%d ... ", port)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%v", port),
		Handler: mux,
	}
	server.ListenAndServe()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}
}

func init() {
	proxyCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	proxyCmd.PersistentFlags().StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	proxyCmd.PersistentFlags().StringVar(&corednsAddress, "coredns", "", "The address of coredns.")
	proxyCmd.PersistentFlags().IntVar(&healthPort, "healthport", 12345, "The port of the health check address.")
	proxyCmd.PersistentFlags().StringVar(&etcdKeyFile, "etcdkeyfile", "", "Path to etcdkeyfile.")
	proxyCmd.PersistentFlags().StringVar(&etcdCertFile, "etcdcertfile", "", "Path to etcdcertfile.")
	proxyCmd.PersistentFlags().StringVar(&etcdCaCertile, "etcdcacertfile", "", "Path to etcdcacertfile.")
	proxyCmd.PersistentFlags().StringVar(&etcdEndpoints, "etcdendpoints", "", "Path to etcdendpoints.")

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(version.CobraCommand())

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Rpc Controller",
		Section: "rpc-controller CLI",
		Manual:  "Istio Rpc Controller",
	}))
}
