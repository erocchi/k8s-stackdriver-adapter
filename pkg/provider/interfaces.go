/*
Copyright 2017 The Kubernetes Authors.

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

package provider

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/metrics/pkg/apis/events"
)

// MetricInfo describes a metric for a particular
// fully-qualified group resource.
type EventInfo struct {
	GroupResource          schema.GroupResource
	Namespaced             bool
	EventName              string
}

// CustomMetricsProvider is a source of custom metrics
// which is able to supply a list of available metrics,
// as well as metric values themselves on demand.
//
// Note that group-resources are provided  as GroupResources,
// not GroupKinds.  This is to allow flexibility on the part
// of the implementor: implementors do not necessarily need
// to be aware of all existing kinds and their corresponding
// REST mappings in order to perform queries.
//
// For queries that use label selectors, it is up to the
// implementor to decide how to make use of the label selector --
// they may wish to query the main Kubernetes API server, or may
// wish to simply make use of stored information in their TSDB.
type EventsProvider interface {

	// GetNamespacedMetricByName fetches a particular metric for a particular namespaced object.
	GetNamespacedEventByName(groupResource schema.GroupResource, namespace string, name string, eventName string) (*events.EventValue, error)



	// ListAllMetrics provides a list of all available metrics at
	// the current time.  Note that this is not allowed to return
	// an error, so it is reccomended that implementors cache and
	// periodically update this list, instead of querying every time.
	//TODO  : ListAllMetrics() []MetricInfo
}
