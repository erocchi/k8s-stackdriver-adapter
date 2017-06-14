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

/*
 * TODO(kawych):
 * - Return one item per resource. Currently one item per resource is returned, but the metric
 *   value is equal to aggregated metric values from multiple time series.
 * - Don't hardcode resource type names.
 */

import (
	"time"
	"fmt"
	"strings"

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

	rateInterval time.Duration
}

// TODO(kawych) think of something better than hardcoding
var objectKinds map[string]string = map[string]string{
	"Pod": "pod",
	"Service": "service",
	"Namespace": "ns",
	"Deployment": "deployment",
}

var objectKinds_v2 map[string]string = map[string]string{
	"Pod": "pod",
	"Service": "service",
	"Namespace": "namespace",
	"Deployment": "deployment",
}

func NewFakeProvider(client coreclient.CoreV1Interface, rateInterval time.Duration) provider.CustomMetricsProvider {
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
		rateInterval: rateInterval,
	}
}

func matchesFilter(timeSeries *stackdriver.TimeSeries, filter map[string]string) bool {
	for k, v := range filter {
		if timeSeries.Metric.Labels[k] != v {
			return false
		}
	}
	return true
}

func getMetricValueFromResponse(response stackdriver.ListTimeSeriesResponse, filter map[string]string) (int64, error) {
	if len(response.TimeSeries) < 1 {
		return 0, fmt.Errorf("Expected at least one time series from Stackdriver, but received %v", len(response.TimeSeries))
	}
	// Find time series with specified labels matching
	// Stackdriver API doesn't allow complex label filtering (i.e. "label1 = x AND (label2 = y OR label2 = z)"),
	// therefore only part of the filters is passed and remaining filtering is done here.
	for _, series := range response.TimeSeries {
		if matchesFilter(series, filter) {
			if len(series.Points) != 1 {
				return 0, fmt.Errorf("Expected exactly one Point in TimeSeries from Stackdriver, but received %v", len(series.Points))
			}
			value := *series.Points[0].Value
			switch {
			case value.Int64Value != nil:
				return *value.Int64Value, nil
			case value.DoubleValue != nil:
				return int64(*value.DoubleValue), nil
			default:
				return 0, fmt.Errorf("Expected metric of type DoubleValue or Int64Value, but received TypedValue: %v", value)
			}
		}
	}
	return 0, fmt.Errorf("Received %v time series from Stackdriver, but non of them matches filters. Series here: %s, filters here: %s", len(response.TimeSeries), response.TimeSeries, filter)
}

func (p* incrementalTestingProvider) groupByFieldsForResource(namespace string) []string {
	if namespace == "" {
		return []string{"metric.label.type"}
	}
		return []string{"metric.label.type", "metric.label.namespace_name"}
}

func (p* incrementalTestingProvider) metricFilterForResource(groupResource schema.GroupResource, name []string, metricName string) (string, error) {
	group, err := api.Registry.Group(groupResource.Group)
	if err != nil {
		return "", err
	}
	kind, err := api.Registry.RESTMapper().KindFor(groupResource.WithVersion(group.GroupVersion.Version))
	if err != nil {
		glog.Errorf("valueFor failed secondly with: %s", err)
		return "", err
	}
	fullMetricName := fmt.Sprintf("%s/%s", p.config.MetricsPrefix, metricName)
	metricFilter := fmt.Sprintf("metric.type = \"%s\"", fullMetricName)

	var nameFilters []string = make([]string, len(name))
	for i := 0; i < len(name); i++ {
		nameFilters[i] = fmt.Sprintf("metric.label.%s_name = \"%s\"", objectKinds_v2[kind.Kind], name[i])
	}
	nameFilter := strings.Join(nameFilters, " OR ")
	return fmt.Sprintf("(%s) AND (%s)", metricFilter, nameFilter), nil
}

func (p* incrementalTestingProvider) additionalFilterForResource(groupResource schema.GroupResource, namespace string) (map[string]string, error) {
	group, err := api.Registry.Group(groupResource.Group)
	if err != nil {
		return nil, err
	}
	kind, err := api.Registry.RESTMapper().KindFor(groupResource.WithVersion(group.GroupVersion.Version))
	if err != nil {
		glog.Errorf("valueFor failed secondly with: %s", err)
		return nil, err
	}
	if namespace == "" {
		return map[string]string{"type": objectKinds[kind.Kind], "namespace_name": namespace}, nil
	}
	return map[string]string{"type": objectKinds[kind.Kind]}, nil
}

func (p *incrementalTestingProvider) valueFor(groupResource schema.GroupResource, metricName string, namespaced bool, metricFilter string, groupByFields []string, additionalFilter map[string]string) (int64, error) {
	project := fmt.Sprintf("projects/%s", p.config.Project)
	endTime := time.Now()
	startTime := endTime.Add(-p.rateInterval)
	request := p.service.Projects.TimeSeries.List(project)
	request = request.Filter(metricFilter)
	request = request.IntervalStartTime(startTime.Format("2006-01-02T15:04:05Z")).IntervalEndTime(endTime.Format("2006-01-02T15:04:05Z"))
	// TODO(kawych): don't use cross-series reducer
	request = request.AggregationPerSeriesAligner("ALIGN_MEAN").AggregationAlignmentPeriod(fmt.Sprintf("%vs", int64(p.rateInterval.Seconds()))).AggregationCrossSeriesReducer("REDUCE_MEAN")
	request = request.AggregationGroupByFields(groupByFields...)
	foo, err := request.Do()
	if err != nil {
		return 0, err
	}

	value, err := getMetricValueFromResponse(*foo, additionalFilter)
	if err != nil {
		return 0, err
	}

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
		Value: *resource.NewMilliQuantity(value * 1000, resource.DecimalSI),
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
		res[i].Value = *resource.NewMilliQuantity(1000 * totalValue / int64(len(res)), resource.DecimalSI)
	}

	//return p.metricFor(value, groupResource, "", name, metricName)
	return &custom_metrics.MetricValueList{
		Items: res,
	}, nil
}

func (p *incrementalTestingProvider) GetRootScopedMetricByName(groupResource schema.GroupResource, name string, metricName string) (*custom_metrics.MetricValue, error) {
	metricsFilter, err := p.metricFilterForResource(groupResource, []string{name}, metricName)
	if err != nil {
		return nil, err
	}
	groupByFields := []string{"metric.label.type"}
	additionalFilter, err := p.additionalFilterForResource(groupResource, "")
	if err != nil {
		return nil, err
	}
	value, err := p.valueFor(groupResource, metricName, false, metricsFilter, groupByFields, additionalFilter)
	if err != nil {
		return nil, err
	}
	return p.metricFor(value, groupResource, "", name, metricName)
}

func (p *incrementalTestingProvider) GetRootScopedMetricBySelector(groupResource schema.GroupResource, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
	// TODO: work for objects not in core v1
	matchingObjectsRaw, err := p.client.RESTClient().Get().
			Resource(groupResource.Resource).
			VersionedParams(&metav1.ListOptions{LabelSelector: selector.String()}, scheme.ParameterCodec).
			Do().
			Get()
	if err != nil {
		return nil, err
	}
	resourceNames, err := getResourceNames(matchingObjectsRaw)
	if err != nil {
		return nil, err
	}
	metricsFilter, err := p.metricFilterForResource(groupResource, resourceNames, metricName)
	if err != nil {
		return nil, err
	}
	groupByFields := []string{"metric.label.type"}
	additionalFilter, err := p.additionalFilterForResource(groupResource, "")
	if err != nil {
		return nil, err
	}
	totalValue, err := p.valueFor(groupResource, metricName, false, metricsFilter, groupByFields, additionalFilter)
	if err != nil {
		return nil, err
	}
	return p.metricsFor(totalValue, groupResource, metricName, matchingObjectsRaw)
}

func (p *incrementalTestingProvider) GetNamespacedMetricByName(groupResource schema.GroupResource, namespace string, name string, metricName string) (*custom_metrics.MetricValue, error) {
	metricsFilter, err := p.metricFilterForResource(groupResource, []string{name}, metricName)
	if err != nil {
		return nil, err
	}
	groupByFields := []string{"metric.label.type", "metric.label.namespace_name"}
	additionalFilter, err := p.additionalFilterForResource(groupResource, namespace)
	if err != nil {
		return nil, err
	}

	value, err := p.valueFor(groupResource, metricName, true, metricsFilter, groupByFields, additionalFilter)
	if err != nil {
		return nil, err
	}

	return p.metricFor(value, groupResource, namespace, name, metricName)
}

func (p *incrementalTestingProvider) GetNamespacedMetricBySelector(groupResource schema.GroupResource, namespace string, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
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
	resourceNames, err := getResourceNames(matchingObjectsRaw)
	if err != nil {
		return nil, err
	}

	metricsFilter, err := p.metricFilterForResource(groupResource, resourceNames, metricName)
	if err != nil {
		return nil, err
	}
	groupByFields := []string{"metric.label.type", "metric.label.namespace_name"}
	additionalFilter, err := p.additionalFilterForResource(groupResource, namespace)
	if err != nil {
		return nil, err
	}

	totalValue, err := p.valueFor(groupResource, metricName, true, metricsFilter, groupByFields, additionalFilter)
	if err != nil {
		return nil, err
	}
	return p.metricsFor(totalValue, groupResource, metricName, matchingObjectsRaw)
}

// TODO(kawych): add proper implementation
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

func getResourceNames(list runtime.Object) ([]string, error) {
	resourceNames := []string{}
	err := apimeta.EachListItem(list, func(item runtime.Object) error {
		objMeta := item.(metav1.ObjectMetaAccessor).GetObjectMeta()
		resourceNames = append(resourceNames, objMeta.GetName())
		return nil
	})
	if err == nil {
		glog.Infof("resource names: ", resourceNames)
	}
	return resourceNames, err
}
