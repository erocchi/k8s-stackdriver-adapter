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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/k8s-stackdriver-adapter/pkg/types"

)

// MetricInfo describes a metric for a particular
// fully-qualified group resource.
type MetricInfo struct {
	GroupResource          schema.GroupResource
	Namespaced             bool
	Metric                 string
}

// EventsProvider is a source of custom metrics
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
	GetNamespacedEventsByName( namespace, eventName string) (*types.EventValue, error)

}