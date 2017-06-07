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
	"time"
	"fmt"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/metrics/pkg/apis/custom_metrics"
	"k8s.io/client-go/pkg/api"
	_ "k8s.io/client-go/pkg/api/install"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/scheme"
	stackdriver "google.golang.org/api/monitoring/v3"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"k8s.io/custom-metrics-boilerplate/pkg/provider"
	"k8s.io/custom-metrics-boilerplate/config"
)

type incrementalTestingProvider struct {
	client coreclient.CoreV1Interface

	values map[provider.MetricInfo]int64

	service *stackdriver.Service

	config *config.GceConfig
}

func NewFakeProvider(client coreclient.CoreV1Interface) provider.CustomMetricsProvider {
	// TODO(kawych): move this part to some sensible place
	oauthClient := oauth2.NewClient(oauth2.NoContext, google.ComputeTokenSource(""))
	stackdriverService, err := stackdriver.New(oauthClient)
	if err != nil {
		glog.Fatalf("Failed to create Stackdriver client: %v", err)
	}
	gceConf, err := config.GetGceConfig("custom.googleapis.com")
	if err != nil {
		glog.Fatalf("Failed to get GCE config: %v", err)
	}
	return &incrementalTestingProvider{
		client: client,
		values: make(map[provider.MetricInfo]int64),
		service: stackdriverService,
		config: gceConf,
	}
}

func (p *incrementalTestingProvider) valueFor(groupResource schema.GroupResource, metricName string, namespaced bool, namespace string, name string) (int64, error) {
	// TODO(kawych) extract this and do it in one place only! START
	group, err := api.Registry.Group(groupResource.Group)
	if err != nil {
		glog.Infof("valueFor failed with: %s", err)
		return 0, err
	}
	kind, err := api.Registry.RESTMapper().KindFor(groupResource.WithVersion(group.GroupVersion.Version))
	if err != nil {
		glog.Infof("valueFor failed secondly with: %s", err)
		return 0, err
	}
	// ******** END ********
	project := fmt.Sprintf("projects/%s", p.config.Project)
	fullMetricName := fmt.Sprintf("%s/%s", p.config.MetricsPrefix, metricName)
	metricFilter := fmt.Sprintf("metric.type = \"%s\"", fullMetricName)
	namespaceFilter := fmt.Sprintf("metric.label.namespace_name = \"%s\"", namespace)
	resourceTypeFilter := fmt.Sprintf("metric.label.type = \"%s\"", kind.Kind)
	// TODO(kawych) namespace, metric.lable.type may be "ns"...
	resourceNameFilter := fmt.Sprintf("metric.label.pod_name = \"%s\"", name)
	// TODO(kawych) may be as well namespace_name/service_name/deployment_name/etc...
	mergedFilters := fmt.Sprintf("(%s) AND (%s) AND (%s) AND (%s)", metricFilter, namespaceFilter, resourceTypeFilter, resourceNameFilter)
	//rable := [4]string{metricFilter, namespaceFilter, resourceTypeFilter, resourceNameFilter}
	//mergedFilters := strings.Join(rable, " AND ")
	foo, err :=
		// TODO(kawych): normal timestamps, normal alignment period...
		p.service.Projects.TimeSeries.List(project).Filter(mergedFilters).IntervalStartTime("2017-06-01T15:00:00Z").IntervalEndTime("2017-06-01T14:00:00Z").AggregationPerSeriesAligner("ALIGN_MEAN").AggregationAlignmentPeriod("3600s").Do()
	if err != nil {
		glog.Fatalf("Failed request to stackdriver api: %s", err)
	}

	value := int64(*foo.TimeSeries[0].Points[0].Value.DoubleValue) // TODO(kawych) make sure that this value is correct

	//metrics := make([]provider.MetricInfo, len(foo.MetricDescriptors))
	glog.Infof("value for metric, groupResource: %s, metricName: %s, namespaced: %v", groupResource, metricName, namespaced)
	info := provider.MetricInfo{
		GroupResource: groupResource,
		Metric: metricName,
		Namespaced: namespaced,
	}

	p.values[info] = value

	return value, nil
}

func (p *incrementalTestingProvider) metricFor(value int64, groupResource schema.GroupResource, namespace string, name string, metricName string) (*custom_metrics.MetricValue, error) {
	group, err := api.Registry.Group(groupResource.Group)
	if err != nil {
		return nil, err
	}
	kind, err := api.Registry.RESTMapper().KindFor(groupResource.WithVersion(group.GroupVersion.Version))
	if err != nil {
		return nil, err
	}

	return &custom_metrics.MetricValue{
		DescribedObject: api.ObjectReference{
			APIVersion: groupResource.Group+"/"+runtime.APIVersionInternal,
			Kind: kind.Kind,
			Name: name,
			Namespace: namespace,
		},
		MetricName: metricName,
		Timestamp: metav1.Time{time.Now()},
		Value: *resource.NewMilliQuantity(value * 100, resource.DecimalSI),
	}, nil
}

func (p *incrementalTestingProvider) metricsFor(totalValue int64, groupResource schema.GroupResource, metricName string, list runtime.Object) (*custom_metrics.MetricValueList, error) {
	if !apimeta.IsListType(list) {
		return nil, fmt.Errorf("returned object was not a list")
	}

	res := make([]custom_metrics.MetricValue, 0)

	err := apimeta.EachListItem(list, func(item runtime.Object) error {
		objMeta := item.(metav1.ObjectMetaAccessor).GetObjectMeta()
		value, err := p.metricFor(0, groupResource, objMeta.GetNamespace(), objMeta.GetName(), metricName)
		if err != nil {
			return err
		}
		res = append(res, *value)

		return nil
	})
	if err != nil {
		return nil, err
	}

	for i := range res {
		res[i].Value = *resource.NewMilliQuantity(100 * totalValue / int64(len(res)), resource.DecimalSI)
	}

	//return p.metricFor(value, groupResource, "", name, metricName)
	return &custom_metrics.MetricValueList{
		Items: res,
	}, nil
}

func (p *incrementalTestingProvider) GetRootScopedMetricByName(groupResource schema.GroupResource, name string, metricName string) (*custom_metrics.MetricValue, error) {
	glog.Infof("getrootscopedmetricbyname, groupResource: %s, metricName: %s, name: %s", groupResource, metricName, name)
	value, err := p.valueFor(groupResource, metricName, false, "", name)
	if err != nil {
		return nil, err
	}
	return p.metricFor(value, groupResource, "", name, metricName)
}


func (p *incrementalTestingProvider) GetRootScopedMetricBySelector(groupResource schema.GroupResource, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
	glog.Infof("getrootscopedmetricbyselector, groupResource: %s, metricName: %s", groupResource, metricName)
	totalValue, err := p.valueFor(groupResource, metricName, false, "", "tratatata") // TODO(kawych) retrieve the actual name
	if err != nil {
		return nil, err
	}

	// TODO: work for objects not in core v1
	matchingObjectsRaw, err := p.client.RESTClient().Get().
			Resource(groupResource.Resource).
			VersionedParams(&metav1.ListOptions{LabelSelector: selector.String()}, scheme.ParameterCodec).
			Do().
			Get()
	if err != nil {
		return nil, err
	}
	return p.metricsFor(totalValue, groupResource, metricName, matchingObjectsRaw)
}

func (p *incrementalTestingProvider) GetNamespacedMetricByName(groupResource schema.GroupResource, namespace string, name string, metricName string) (*custom_metrics.MetricValue, error) {
	glog.Infof("getnamespacedmetricbyname, groupResource: %s, namespace: %s, metricName: %s, name: %s", groupResource, namespace, metricName, name)
	value, err := p.valueFor(groupResource, metricName, true, namespace, name)
	if err != nil {
		return nil, err
	}
	return p.metricFor(value, groupResource, namespace, name, metricName)
}

func (p *incrementalTestingProvider) GetNamespacedMetricBySelector(groupResource schema.GroupResource, namespace string, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
	glog.Infof("getnamespacedmetricbyselector, groupResource: %s, namespace: %s, metricName: %s", groupResource, namespace, metricName)
	totalValue, err := p.valueFor(groupResource, metricName, true, namespace, "tratatata") // TODO(kawych) retrieve the actual name
	if err != nil {
		return nil, err
	}

	// TODO: work for objects not in core v1
	matchingObjectsRaw, err := p.client.RESTClient().Get().
			Namespace(namespace).
			Resource(groupResource.Resource).
			VersionedParams(&metav1.ListOptions{LabelSelector: selector.String()}, scheme.ParameterCodec).
			Do().
			Get()
	if err != nil {
		return nil, err
	}
	return p.metricsFor(totalValue, groupResource, metricName, matchingObjectsRaw)
}

func (p *incrementalTestingProvider) ListAllMetrics() []provider.MetricInfo {
	// TODO(kawych)
	// - filter only type GAUGE (so that we can aggregate)
	// - assign to relevant resource types
	// - ...
	glog.Infof("listing all metrics, project: %s, cluster: %s, metric prefix: %s", p.config.Project, p.config.Cluster, p.config.MetricsPrefix)
	project := fmt.Sprintf("projects/%s", p.config.Project)
	onlyCustom := fmt.Sprintf("metric.type = starts_with(\"%s/\")", p.config.MetricsPrefix)
	foo, err := p.service.Projects.MetricDescriptors.List(project).Filter(onlyCustom).Do() //TODO(kawych) support errors
	if err != nil {
		glog.Fatalf("Failed request to stackdriver api: %s", err)
	}
	metrics := make([]provider.MetricInfo, len(foo.MetricDescriptors))

	for i := 0; i < len(foo.MetricDescriptors); i++ {
		namespaced := false
		for j := 0; j < len(foo.MetricDescriptors[i].Labels); j++ {
			if foo.MetricDescriptors[i].Labels[j].Key == "namespace_name" {
				namespaced = true
			}
		}
		metrics[i] = provider.MetricInfo{
			// Resource: pods/services/namespaces/deployments/...
			GroupResource: schema.GroupResource{Group: "", Resource: "pods"},
			Metric: foo.MetricDescriptors[i].Type,
			Namespaced: namespaced,
		}
	}
	return metrics
}
