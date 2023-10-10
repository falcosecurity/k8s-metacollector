// Copyright 2023 The Falco Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metadata

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	serverSubsystem = "meta_collector_server"
	subscribersKey  = "subscribers"
)

var (
	subscribers = prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: serverSubsystem,
		Name:      subscribersKey,
		Help:      "Number of subscribers.",
	})
)

func init() {
	ctrlmetrics.Registry.MustRegister(subscribers)
}
