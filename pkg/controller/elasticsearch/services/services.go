// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package services

import (
	"fmt"
	"math/rand"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/elastic/cloud-on-k8s/pkg/apis/elasticsearch/v1alpha1"
	"github.com/elastic/cloud-on-k8s/pkg/controller/common/defaults"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/name"
	"github.com/elastic/cloud-on-k8s/pkg/controller/elasticsearch/network"
	"github.com/elastic/cloud-on-k8s/pkg/utils/k8s"
	"github.com/elastic/cloud-on-k8s/pkg/utils/stringsutil"
)

const (
	globalServiceSuffix = ".svc"
)

// ExternalServiceName returns the name for the external service
// associated to this cluster
func ExternalServiceName(esName string) string {
	return name.HTTPService(esName)
}

// ExternalServiceURL returns the URL used to reach Elasticsearch's external endpoint
func ExternalServiceURL(es v1alpha1.Elasticsearch) string {
	return stringsutil.Concat(es.Spec.HTTP.Scheme(), "://", ExternalServiceName(es.Name), ".", es.Namespace, globalServiceSuffix, ":", strconv.Itoa(network.HTTPPort))
}

// NewExternalService returns the external service associated to the given cluster
// It is used by users to perform requests against one of the cluster nodes.
func NewExternalService(es v1alpha1.Elasticsearch) *corev1.Service {
	nsn := k8s.ExtractNamespacedName(&es)

	svc := corev1.Service{
		ObjectMeta: es.Spec.HTTP.Service.ObjectMeta,
		Spec:       es.Spec.HTTP.Service.Spec,
	}

	svc.ObjectMeta.Namespace = es.Namespace
	svc.ObjectMeta.Name = ExternalServiceName(es.Name)

	labels := label.NewLabels(nsn)
	ports := []corev1.ServicePort{
		{
			Name:     "https",
			Protocol: corev1.ProtocolTCP,
			Port:     network.HTTPPort,
		},
	}

	return defaults.SetServiceDefaults(&svc, labels, labels, ports)
}

// IsServiceReady checks if a service has one or more ready endpoints.
func IsServiceReady(c k8s.Client, service corev1.Service) (bool, error) {
	endpoints := corev1.Endpoints{}
	namespacedName := types.NamespacedName{Namespace: service.Namespace, Name: service.Name}

	if err := c.Get(namespacedName, &endpoints); err != nil {
		return false, err
	}
	for _, subs := range endpoints.Subsets {
		if len(subs.Addresses) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// GetExternalService returns the external service associated to the given Elasticsearch cluster.
func GetExternalService(c k8s.Client, es v1alpha1.Elasticsearch) (corev1.Service, error) {
	var svc corev1.Service

	namespacedName := types.NamespacedName{
		Namespace: es.Namespace,
		Name:      ExternalServiceName(es.Name),
	}

	if err := c.Get(namespacedName, &svc); err != nil {
		return corev1.Service{}, err
	}

	return svc, nil
}

// ElasticsearchURL calculates the base url for Elasticsearch, taking into account the currently running pods.
// If there is an HTTP scheme mismatch between spec and pods we switch to requesting individual pods directly
// otherwise this delegates to ExternalServiceURL.
func ElasticsearchURL(es v1alpha1.Elasticsearch, pods []corev1.Pod) string {
	var schemeChange bool
	for _, p := range pods {
		scheme, exists := p.Labels[label.HTTPSchemeLabelName]
		if exists && scheme != es.Spec.HTTP.Scheme() {
			// scheme in existing pods does not match scheme in spec, user toggled HTTP(S)
			schemeChange = true
		}
	}
	if schemeChange {
		// switch to sending requests directly to a random pod instead of going through the service
		randomPod := pods[rand.Intn(len(pods))]
		scheme, hasScheme := randomPod.Labels[label.HTTPSchemeLabelName]
		sset, hasSset := randomPod.Labels[label.StatefulSetNameLabelName]
		if hasScheme && hasSset {
			return fmt.Sprintf("%s://%s.%s.%s:%d", scheme, randomPod.Name, sset, randomPod.Namespace, network.HTTPPort)
		}
	}
	return ExternalServiceURL(es)
}
