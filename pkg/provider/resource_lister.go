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
	"k8s.io/apiserver/pkg/endpoints/handlers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type eventResourceLister struct {
	provider EventsProvider
}

func NewResourceLister(provider EventsProvider) handlers.APIResourceLister {
	return &eventsResourceLister{
		provider: provider,
	}
}

func (l *eventsResourceLister) ListAPIResources() []metav1.APIResource {
	/*metrics := l.provider.ListAllMetrics()
	resources := make([]metav1.APIResource, len(metrics))

	for i, metric := range metrics {
		resources[i] = metav1.APIResource{
			Name: metric.GroupResource.String()+"/"+metric.Metric,
			Namespaced: metric.Namespaced,
			Kind: "MetricValueList",
			Verbs: metav1.Verbs{"get"}, // TODO: support "watch"
		}
	}

	return resources*/
	resources := make([]metav1.APIResource,1)
	resources[0] = metav1.APIResource{
		Name: v1events,
		Namespaced: "default",
		Kind: "EventValueList",
		Verbs: metav1.Verbs{"get"},
	}

}
