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
 * - support other apis - currently only api v1 supported (e.g. Deployments are in extensions/v1beta1)
 */

import (
	//"context"
	"time"
	"fmt"
	"strings"
	"encoding/json"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
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

	// for easier faking:
	//monitoring "cloud.google.com/go/monitoring/apiv3"

	"k8s.io/k8s-stackdriver-adapter/pkg/provider"
	"k8s.io/k8s-stackdriver-adapter/pkg/config"
)

type stackdriverProvider struct {
	//client coreclient.CoreV1Interface
	restClient rest.Interface

	values map[provider.MetricInfo]map[string]int64

	service *stackdriver.Service

	config *config.GceConfig

	// TODO(kawych): maybe another interval for refreshing cache?
	// TODO(kawych): maybe we should have a cache at all?
	rateInterval time.Duration

	// TODO(kawych): think about when should it be obtained...
	resourceNamer map[string]map[string]bool
}

func NewStackdriverProvider(restClient rest.Interface, rateInterval time.Duration) provider.CustomMetricsProvider {
	//monClient := monitoring.NewMetricClient(context.Background())
	// TODO(kawych): move this part to some sensible place
	oauthClient := oauth2.NewClient(oauth2.NoContext, google.ComputeTokenSource(""))
	stackdriverService, err := stackdriver.New(oauthClient)
	if err != nil {
		glog.Fatalf("Failed to create Stackdriver client: %v", err)
	}
	// TODO(kawych): decide on prefix. Does it have to be gce?
	gceConf, err := config.GetGceConfig("custom.googleapis.com")
	if err != nil {
		glog.Fatalf("Failed to get GCE config: %v", err)
	}
	namer, err := getResourceNamer(restClient)
	if err != nil {
		glog.Fatalf("Failed to create resource namer")
	}
	return &stackdriverProvider{
		restClient: restClient,
		values: make(map[provider.MetricInfo]map[string]int64),
		service: stackdriverService,
		config: gceConf,
		rateInterval: rateInterval,
		resourceNamer: namer,
	}
}

func (p *stackdriverProvider) getMetricValuesFromResponse(groupResource schema.GroupResource, namespace string, response stackdriver.ListTimeSeriesResponse) (map[string]int64, error) {
	group, err := api.Registry.Group(groupResource.Group)
	if err != nil {
		return nil, err
	}
	kind, err := api.Registry.RESTMapper().KindFor(groupResource.WithVersion(group.GroupVersion.Version))
	if err != nil {
		return nil, err
	}
	if len(response.TimeSeries) < 1 {
		return nil, fmt.Errorf("Expected at least one time series from Stackdriver, but received %v", len(response.TimeSeries))
	}
	metricValues := make(map[string]int64)
	// Find time series with specified labels matching
	// Stackdriver API doesn't allow complex label filtering (i.e. "label1 = x AND (label2 = y OR label2 = z)"),
	// therefore only part of the filters is passed and remaining filtering is done here.
	for _, series := range response.TimeSeries {
		ok, err := p.matchesAdditionalRequirements(groupResource, namespace, series)
		if err != nil {
			return nil, err
		}
		if ok {
			if len(series.Points) != 1 {
				return nil, fmt.Errorf("Expected exactly one Point in TimeSeries from Stackdriver, but received %v", len(series.Points))
			}
			value := *series.Points[0].Value
			name := series.Metric.Labels[fmt.Sprintf("%s_name", strings.ToLower(kind.Kind))]
			switch {
			case value.Int64Value != nil:
				metricValues[name] = *value.Int64Value
			case value.DoubleValue != nil:
				metricValues[name] = int64(*value.DoubleValue)
			default:
				return nil, fmt.Errorf("Expected metric of type DoubleValue or Int64Value, but received TypedValue: %v", value)
			}
		}
	}
	return metricValues, nil
	//return 0, fmt.Errorf("Received %v time series from Stackdriver, but non of them matches filters. Series here: %s, groupResource = %s, namespace = %s", len(response.TimeSeries), response.TimeSeries, groupResource, namespace)
}

func (p*stackdriverProvider) groupByFieldsForResource(namespace string) []string {
	if namespace == "" {
		return []string{"metric.label.type"}
	}
		return []string{"metric.label.type", "metric.label.namespace_name"}
}

func getResourceNamer(restClient rest.Interface) (map[string]map[string]bool, error) {
	rawData, err := restClient.Get().Do().Raw()
	if err != nil {
		return nil, err
	}
	resultMap := map[string]map[string]bool{}
	var rawMap map[string]interface{}
	err = json.Unmarshal(rawData, &rawMap)
	if err != nil {
		return nil, err
	}
	resourcesList, ok := rawMap["resources"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("conversion error0")
	}
	for _, resource := range resourcesList {
		resource, ok := resource.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("conversion error1")
		}
		resourceKind, ok := resource["kind"].(string)
		if !ok {
			return nil, fmt.Errorf("conversion error2")
		}
		resourceName := strings.ToLower(resourceKind)
		shortNames, ok := resource["shortNames"].([]interface{})
		if !ok {
			shortNames = []interface{}{}
		}
		allNames := map[string]bool{resourceName: true}
		// Preserve shortcuts that are already there
		if current, ok := resultMap[resourceKind]; ok {
			for k, v := range current {
				allNames[k] = v
			}
		}
		for _, shortName := range shortNames {
			shortName, ok := shortName.(string)
			if !ok {
				return nil, fmt.Errorf("conversion error3")
			}
			allNames[shortName] = true
		}
		resultMap[resourceKind] = allNames
	}
	return resultMap, nil
}

func (p*stackdriverProvider) metricFilterForResource(groupResource schema.GroupResource, name []string, metricName string) (string, error) {
	group, err := api.Registry.Group(groupResource.Group)
	if err != nil {
		return "", err
	}
	kind, err := api.Registry.RESTMapper().KindFor(groupResource.WithVersion(group.GroupVersion.Version))
	if err != nil {
		return "", err
	}
	fullMetricName := fmt.Sprintf("%s/%s", p.config.MetricsPrefix, metricName)
	metricFilter := fmt.Sprintf("metric.type = \"%s\"", fullMetricName)

	var nameFilters []string = make([]string, len(name))
	for i := 0; i < len(name); i++ {
		nameFilters[i] = fmt.Sprintf("metric.label.%s_name = \"%s\"", strings.ToLower(kind.Kind), name[i])
	}
	nameFilter := strings.Join(nameFilters, " OR ")
	return fmt.Sprintf("(%s) AND (%s)", metricFilter, nameFilter), nil
}

func (p*stackdriverProvider) matchesAdditionalRequirements(groupResource schema.GroupResource, namespace string, timeSeries *stackdriver.TimeSeries) (bool, error) {
	group, err := api.Registry.Group(groupResource.Group)
	if err != nil {
		return false, err
	}
	kind, err := api.Registry.RESTMapper().KindFor(groupResource.WithVersion(group.GroupVersion.Version))
	if err != nil {
		glog.Errorf("valueFor failed secondly with: %s", err)
		return false, err
	}
	allowedTypes, ok := p.resourceNamer[kind.Kind]
	if !ok {
		return false, fmt.Errorf("unknown resource: %s", kind.Kind)
	}
	/*if allowedTypes[timeSeries.Metric.Labels["type"]] && (namespace == "" || timeSeries.Metric.Labels["namespace_name"] == namespace) {
		glog.Infof("matches")
		return true, nil
	} else {
		glog.Infof("unmach, %s, '%s' ?= '%s'", allowedTypes[timeSeries.Metric.Labels["type"]], timeSeries.Metric.Labels["namespace_name"], namespace)
		return false, nil
	}*/
	return allowedTypes[timeSeries.Metric.Labels["type"]] && (namespace == "" || timeSeries.Metric.Labels["namespace_name"] == namespace), nil
}

func (p *stackdriverProvider) valuesFor(groupResource schema.GroupResource, metricName string, namespace string, metricFilter string, groupByFields []string) (map[string]int64, error) {
	project := fmt.Sprintf("projects/%s", p.config.Project)
	endTime := time.Now()
	startTime := endTime.Add(-p.rateInterval)
	request := p.service.Projects.TimeSeries.List(project)
	request = request.Filter(metricFilter)
	request = request.IntervalStartTime(startTime.Format("2006-01-02T15:04:05Z")).IntervalEndTime(endTime.Format("2006-01-02T15:04:05Z"))
	request = request.AggregationPerSeriesAligner("ALIGN_MEAN").AggregationAlignmentPeriod(fmt.Sprintf("%vs", int64(p.rateInterval.Seconds())))
	glog.Infof("request following: %s", request)
	foo, err := request.Do()
	if err != nil {
		return nil, err
	}

	values, err := p.getMetricValuesFromResponse(groupResource, namespace, *foo)
	if err != nil {
		return nil, err
	}

	info := provider.MetricInfo{
		GroupResource: groupResource,
		Metric: metricName,
		Namespaced: namespace != "",
	}

	p.values[info] = values

	return values, nil
}

func (p *stackdriverProvider) metricFor(value int64, groupResource schema.GroupResource, namespace string, name string, metricName string) (*custom_metrics.MetricValue, error) {
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

func (p *stackdriverProvider) metricsFor(values map[string]int64, groupResource schema.GroupResource, metricName string, list runtime.Object) (*custom_metrics.MetricValueList, error) {
	if !apimeta.IsListType(list) {
		return nil, fmt.Errorf("returned object was not a list")
	}

	res := make([]custom_metrics.MetricValue, 0)

	err := apimeta.EachListItem(list, func(item runtime.Object) error {
		objMeta := item.(metav1.ObjectMetaAccessor).GetObjectMeta()
		if _, ok := values[objMeta.GetName()]; !ok {
			// TODO(kawych): maybe print better, e.g. "No metric found for Pod: 'blazer' in namespace: 'flasky'"
			return fmt.Errorf("No metric found for object: '%s'", objMeta.GetName())
		}
		value, err := p.metricFor(values[objMeta.GetName()], groupResource, objMeta.GetNamespace(), objMeta.GetName(), metricName)
		if err != nil {
			return err
		}
		res = append(res, *value)

		return nil
	})
	if err != nil {
		return nil, err
	}

	//return p.metricFor(value, groupResource, "", name, metricName)
	return &custom_metrics.MetricValueList{
		Items: res,
	}, nil
}

func (p *stackdriverProvider) GetRootScopedMetricByName(groupResource schema.GroupResource, name string, metricName string) (*custom_metrics.MetricValue, error) {
	metricsFilter, err := p.metricFilterForResource(groupResource, []string{name}, metricName)
	if err != nil {
		return nil, err
	}
	groupByFields := []string{"metric.label.type"}


	values, err := p.valuesFor(groupResource, metricName, "", metricsFilter, groupByFields)
	if err != nil {
		return nil, err
	}
	if _, ok := values[name]; !ok {
		return nil, fmt.Errorf("No metrics found for object: %s", name)
	}
	return p.metricFor(values[name], groupResource, "", name, metricName)
}

func (p *stackdriverProvider) GetRootScopedMetricBySelector(groupResource schema.GroupResource, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
	/*reqs, ok := selector.Requirements()
	for _, req := range reqs {
		req.Operator()
	}*/
	// TODO: work for objects not in core v1
	matchingObjectsRaw, err := p.restClient.Get().
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
	if len(resourceNames) == 0 {
		return nil, fmt.Errorf("No objects found for selector %s", selector)
	}
	metricsFilter, err := p.metricFilterForResource(groupResource, resourceNames, metricName)
	if err != nil {
		return nil, err
	}
	groupByFields := []string{"metric.label.type"}

	values, err := p.valuesFor(groupResource, metricName, "", metricsFilter, groupByFields)
	if err != nil {
		return nil, err
	}
	return p.metricsFor(values, groupResource, metricName, matchingObjectsRaw)
}

func (p *stackdriverProvider) GetNamespacedMetricByName(groupResource schema.GroupResource, namespace string, name string, metricName string) (*custom_metrics.MetricValue, error) {
	metricsFilter, err := p.metricFilterForResource(groupResource, []string{name}, metricName)
	if err != nil {
		return nil, err
	}
	groupByFields := []string{"metric.label.type", "metric.label.namespace_name"}

	values, err := p.valuesFor(groupResource, metricName, namespace, metricsFilter, groupByFields)
	if err != nil {
		return nil, err
	}
	if _, ok := values[name]; !ok {
		return nil, fmt.Errorf("not got name: %s", name)
	}
	return p.metricFor(values[name], groupResource, namespace, name, metricName)
}

func (p *stackdriverProvider) GetNamespacedMetricBySelector(groupResource schema.GroupResource, namespace string, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
	// TODO: work for objects not in core v1
	matchingObjectsRaw, err := p.restClient.Get().
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
	if len(resourceNames) == 0 {
		return nil, fmt.Errorf("No objects found for selector %s", selector)
	}
	metricsFilter, err := p.metricFilterForResource(groupResource, resourceNames, metricName)
	if err != nil {
		return nil, err
	}
	groupByFields := []string{"metric.label.type", "metric.label.namespace_name"}

	values, err := p.valuesFor(groupResource, metricName, namespace, metricsFilter, groupByFields)
	if err != nil {
		return nil, err
	}
	return p.metricsFor(values, groupResource, metricName, matchingObjectsRaw)
}

// TODO(kawych): add proper implementation
func (p *stackdriverProvider) ListAllMetrics() []provider.MetricInfo {
	metrics := []provider.MetricInfo{}

	glog.Infof("listing all metrics, project: %s, cluster: %s, metric prefix: %s", p.config.Project, p.config.Cluster, p.config.MetricsPrefix)
	project := fmt.Sprintf("projects/%s", p.config.Project)
	onlyCustom := fmt.Sprintf("metric.type = starts_with(\"%s/\")", p.config.MetricsPrefix)
	foo, err := p.service.Projects.MetricDescriptors.List(project).Filter(onlyCustom).Do() //TODO(kawych) support errors
	if err != nil {
		glog.Errorf("Failed request to stackdriver api: %s", err)
		return metrics
	}

	for _, descriptor := range foo.MetricDescriptors {
		if descriptor.MetricKind == "GAUGE" && descriptor.ValueType == "INT64" || descriptor.ValueType == "DOUBLE" {
			namespaced := false
			for _, labelDescriptor := range descriptor.Labels {
				if labelDescriptor.Key == "namespace_name" {
					namespaced = true
				}
			}
			resources, err := p.fetchMetric(descriptor.Type)
			if err != nil {
				glog.Errorf("unable to fetch metric %s", descriptor.Name)
				return metrics
			}
			for _, resource := range resources {
				metrics = append(metrics, provider.MetricInfo{
					GroupResource: schema.GroupResource{Group: "", Resource: resource},
					Metric: descriptor.Type,
					Namespaced: namespaced,
				})
			}
		}
	}

	return metrics
}

func (p *stackdriverProvider) fetchMetric(metricName string) ([]string, error) {
	project := fmt.Sprintf("projects/%s", p.config.Project)
	metricFilter := fmt.Sprintf("metric.type = \"%s\"", metricName)
	endTime := time.Now()
	startTime := endTime.Add(-p.rateInterval)
	request := p.service.Projects.TimeSeries.List(project)
	request = request.Filter(metricFilter)
	request = request.IntervalStartTime(startTime.Format("2006-01-02T15:04:05Z")).IntervalEndTime(endTime.Format("2006-01-02T15:04:05Z"))
	request = request.AggregationPerSeriesAligner("ALIGN_MEAN").AggregationAlignmentPeriod(fmt.Sprintf("%vs", int64(p.rateInterval.Seconds())))
	request = request.AggregationCrossSeriesReducer("REDUCE_MEAN").AggregationGroupByFields("metric.label.type")
	glog.Infof("request following: %s", request)
	foo, err := request.Do()
	if err != nil {
		return nil, err
	}

	resources := []string{}
	for _, series := range foo.TimeSeries {
		for label, value := range series.Metric.Labels {
			if label == "type" {
				// instead desingaluritarize
				resources = append(resources, value + "s")
			}
		}
	}
	return resources, nil
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
