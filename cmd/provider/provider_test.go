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
	//"fmt"
	//"sort"
	//"time"
	"testing"

	//"k8s.io/client-go/kubernetes/typed/core/v1/fake"
	"k8s.io/k8s-stackdriver-adapter/pkg/provider"
	"github.com/stretchr/testify/assert"
	//"k8s.io/client-go/testing""
	//"google.golang.org/api/monitoring/v3"
)

type fakeStackdriverService struct {
}

//func (s *fakeStackdriverService)

func setupStackdriverProvider(y *testing.T) (provider.EventsProvider, *fakeStackdriverService) {
	//fakeRestClient := fake.FakeCoreV1{
	//	testing.Fake,
	//}.RESTClient()
	//fakeKubeClient := &fake.
	return nil, nil
}//TODO testAPI