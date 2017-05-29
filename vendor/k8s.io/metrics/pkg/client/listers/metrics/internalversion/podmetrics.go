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

// This file was automatically generated by lister-gen

package internalversion

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	metrics "k8s.io/metrics/pkg/apis/metrics"
)

// PodMetricsLister helps list PodMetricses.
type PodMetricsLister interface {
	// List lists all PodMetricses in the indexer.
	List(selector labels.Selector) (ret []*metrics.PodMetrics, err error)
	// PodMetricses returns an object that can list and get PodMetricses.
	PodMetricses(namespace string) PodMetricsNamespaceLister
	PodMetricsListerExpansion
}

// podMetricsLister implements the PodMetricsLister interface.
type podMetricsLister struct {
	indexer cache.Indexer
}

// NewPodMetricsLister returns a new PodMetricsLister.
func NewPodMetricsLister(indexer cache.Indexer) PodMetricsLister {
	return &podMetricsLister{indexer: indexer}
}

// List lists all PodMetricses in the indexer.
func (s *podMetricsLister) List(selector labels.Selector) (ret []*metrics.PodMetrics, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*metrics.PodMetrics))
	})
	return ret, err
}

// PodMetricses returns an object that can list and get PodMetricses.
func (s *podMetricsLister) PodMetricses(namespace string) PodMetricsNamespaceLister {
	return podMetricsNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// PodMetricsNamespaceLister helps list and get PodMetricses.
type PodMetricsNamespaceLister interface {
	// List lists all PodMetricses in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*metrics.PodMetrics, err error)
	// Get retrieves the PodMetrics from the indexer for a given namespace and name.
	Get(name string) (*metrics.PodMetrics, error)
	PodMetricsNamespaceListerExpansion
}

// podMetricsNamespaceLister implements the PodMetricsNamespaceLister
// interface.
type podMetricsNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all PodMetricses in the indexer for a given namespace.
func (s podMetricsNamespaceLister) List(selector labels.Selector) (ret []*metrics.PodMetrics, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*metrics.PodMetrics))
	})
	return ret, err
}

// Get retrieves the PodMetrics from the indexer for a given namespace and name.
func (s podMetricsNamespaceLister) Get(name string) (*metrics.PodMetrics, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(metrics.Resource("podmetrics"), name)
	}
	return obj.(*metrics.PodMetrics), nil
}
