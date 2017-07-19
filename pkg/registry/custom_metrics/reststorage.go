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

package apiserver

import (
	"fmt"

	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/runtime"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	specificinstaller "k8s.io/k8s-stackdriver-adapter/pkg/apiserver/installer/context"
	"k8s.io/k8s-stackdriver-adapter/pkg/provider"
	"k8s.io/metrics/pkg/apis/events"

)

type REST struct {
	evProvider provider.EventsProvider
}

var _ rest.Storage = &REST{}
var _ rest.Lister = &REST{}

func NewREST(evProvider provider.EventsProvider) *REST {
	return &REST{
		evProvider: evProvider,
	}
}

// Implement Storage

func (r *REST) New() runtime.Object {
	return &events.EventValue{}
}

// Implement Lister

func (r *REST) NewList() runtime.Object {
	return &events.EventValueList{}
}


func (r *REST) List(ctx genericapirequest.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {


	namespace := genericapirequest.NamespaceValue(ctx)

	resourceRaw, eventName, ok := specificinstaller.ResourceInformationFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("unable to get events name from request")
	}
	// handle events
	if resourceRaw != "events" {
		return nil, fmt.Errorf("Usage : namespaces/{namespace}/events/{eventName}")
	}
	return GetNamespacedEventsByName(namespace,eventName)
}

func GetNamespacedEventsByName( namespace, eventName string) (*events.EventValue, error){
	return nil,fmt.Errorf("Hello wolrd! namespace: %s eventName: %s ", namespace, eventName)

}