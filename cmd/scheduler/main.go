/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"k8s.io/component-base/cli"
	_ "k8s.io/component-base/metrics/prometheus/clientgo" // for rest client metric registration
	_ "k8s.io/component-base/metrics/prometheus/version"  // for version metric registration
	"k8s.io/kubernetes/cmd/kube-scheduler/app"

	"sigs.k8s.io/scheduler-plugins/pkg/capacityscheduling"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler"
	"sigs.k8s.io/scheduler-plugins/pkg/coscheduling"
	"sigs.k8s.io/scheduler-plugins/pkg/networkaware/networkoverhead"
	"sigs.k8s.io/scheduler-plugins/pkg/networkaware/topologicalsort"
	"sigs.k8s.io/scheduler-plugins/pkg/noderesources"
	"sigs.k8s.io/scheduler-plugins/pkg/noderesourcetopology"
	"sigs.k8s.io/scheduler-plugins/pkg/preemptiontoleration"
	"sigs.k8s.io/scheduler-plugins/pkg/sysched"
	"sigs.k8s.io/scheduler-plugins/pkg/trimaran/loadvariationriskbalancing"
	"sigs.k8s.io/scheduler-plugins/pkg/trimaran/lowriskovercommitment"
	"sigs.k8s.io/scheduler-plugins/pkg/trimaran/peaks"
	"sigs.k8s.io/scheduler-plugins/pkg/trimaran/targetloadpacking"

	// Ensure scheme package is initialized.
	_ "sigs.k8s.io/scheduler-plugins/apis/config/scheme"
)

func recordMetrics() {
	go func() {
		for {
			carbonIntensity.Set(100)
			time.Sleep(30 * time.Second)
		}
	}()
}

var (
	carbonIntensity = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "carbonawarescheduler_carbonintensity",
		Help: "The carbon intensity value from ElectrictyMap API",
	})
)

func main() {
	recordMetrics()
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":2112", nil)

	command := app.NewSchedulerCommand(
		app.WithPlugin(capacityscheduling.Name, capacityscheduling.New),
		app.WithPlugin(coscheduling.Name, coscheduling.New),
		app.WithPlugin(loadvariationriskbalancing.Name, loadvariationriskbalancing.New),
		app.WithPlugin(networkoverhead.Name, networkoverhead.New),
		app.WithPlugin(topologicalsort.Name, topologicalsort.New),
		app.WithPlugin(noderesources.AllocatableName, noderesources.NewAllocatable),
		app.WithPlugin(noderesourcetopology.Name, noderesourcetopology.New),
		app.WithPlugin(preemptiontoleration.Name, preemptiontoleration.New),
		app.WithPlugin(targetloadpacking.Name, targetloadpacking.New),
		app.WithPlugin(lowriskovercommitment.Name, lowriskovercommitment.New),
		app.WithPlugin(sysched.Name, sysched.New),
		app.WithPlugin(peaks.Name, peaks.New),
		app.WithPlugin(carbonawarescheduler.Name, carbonawarescheduler.New),
	)

	code := cli.Run(command)
	os.Exit(code)
}
