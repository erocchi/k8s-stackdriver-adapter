#!/bin/bash

# Test cases
# Should be called from within a pod
# e.g. kubectl run -it --tty cmd --image=fedora /bin/sh
curl -k https://uber-adapter:443/apis/custom-metrics.metrics.k8s.io/v1alpha1/namespaces/default/pods/*/kubernetes.io/memory/usage
curl -k https://uber-adapter:443/apis/custom-metrics.metrics.k8s.io/v1alpha1/namespaces/default/pods/*/kubernetes.io/memory/usage?labelSelector=run%3Duber-adapter
# TODO examples
curl -k https://uber-adapter:443/apis/custom-metrics.metrics.k8s.io/v1alpha1/namespaces/default/metrics/kubernetes.io/cpu/request
curl -k https://uber-adapter:443/apis/custom-metrics.metrics.k8s.io/v1alpha1/namespaces/*/kubernetes.io/cpu/request
curl -k https://uber-adapter:443/apis/custom-metrics.metrics.k8s.io/v1alpha1/nodes/*/kubernetes.io/network/tx_rate
curl -k https://uber-adapter:443/apis/custom-metrics.metrics.k8s.io/v1alpha1/nodes/*/kubernetes.io/network/tx_rate?labelSelector=to-do-label%3Dto-do-value
