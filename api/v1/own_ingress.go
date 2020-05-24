package v1

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ingress信息
type OwnIngress struct {
	Domains []string `json:"domain"`
}

// make a new Ingress Object
func (ownIngress *OwnIngress) MakeOwnResource(instance *Unit, logger logr.Logger,
	scheme *runtime.Scheme) (interface{}, error) {

	// new a Ingress object
	ing := &v1beta1.Ingress{
		// metadata field inherited from owner Unit
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace:instance.Namespace, Labels: instance.Labels},
	}

	var rules []v1beta1.IngressRule
	for _, domain := range ownIngress.Domains {
		// 在Unit的使用场景里，ing只用作http流量，且无需复杂的路径判断，统一使用 "/" 路径
		ingressPath := v1beta1.HTTPIngressPath{
			Path: "/",
			Backend: v1beta1.IngressBackend{
				ServiceName: instance.Name,
				ServicePort: intstr.IntOrString{IntVal: 80},
			},
		}

		rule := v1beta1.IngressRule{
			Host: domain,

			IngressRuleValue: v1beta1.IngressRuleValue{
				HTTP: &v1beta1.HTTPIngressRuleValue{
					Paths: []v1beta1.HTTPIngressPath{
						ingressPath,
					},
				},
			},
		}

		rules = append(rules, rule)
	}

	ing.Spec.Rules = rules

	// add ControllerReference for ingress，the owner is Unit object
	if err := controllerutil.SetControllerReference(instance, ing, scheme); err != nil {
		msg := fmt.Sprintf("set controllerReference for Ingress %s/%s failed", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return nil, err
	}

	return ing, nil
}

// Check if the ownIngress already exists
func (ownIngress *OwnIngress) OwnResourceExist(instance *Unit, client client.Client,
	logger logr.Logger) (bool, interface{}, error) {

	found := &v1beta1.Ingress{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil, nil
		}
		msg := fmt.Sprintf("Ingress %s/%s found, but with error: %s  \n", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return true, found, err
	}
	return true, found, nil
}

// Update Unit ownIngress status
func (ownIngress *OwnIngress) UpdateOwnResourceStatus(instance *Unit, client client.Client,
	logger logr.Logger) (*Unit, error) {

	found := &v1beta1.Ingress{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		return instance, err
	}

	instance.Status.RelationResourceStatus.Ingress = found.Spec.Rules
	instance.Status.LastUpdateTime = metav1.Now()

	return instance, nil
}

// apply this own resource, create or update
func (ownIngress *OwnIngress) ApplyOwnResource(instance *Unit, client client.Client,
	logger logr.Logger, scheme *runtime.Scheme) error {

	// assert if Ingress exist
	exist, found, err := ownIngress.OwnResourceExist(instance, client, logger)
	if err != nil {
		return err
	}

	// make Ingress object
	sts, err := ownIngress.MakeOwnResource(instance, logger, scheme)
	if err != nil {
		return err
	}
	newIngress := sts.(*v1beta1.Ingress)

	// apply the Ingress object just make
	if !exist {
		// if Ingress not exist，then create it
		msg := fmt.Sprintf("Ingress %s/%s not found, create it!", newIngress.Namespace, newIngress.Name)
		logger.Info(msg)
		return client.Create(context.TODO(), newIngress)
	} else {
		foundIngress := found.(*v1beta1.Ingress)
		// if Ingress exist with change，then try to update it
		if !reflect.DeepEqual(newIngress.Spec, foundIngress.Spec) {
			msg := fmt.Sprintf("Updating Ingress %s/%s", newIngress.Namespace, newIngress.Name)
			logger.Info(msg)
			return client.Update(context.TODO(), newIngress)
		}
		return nil
	}
}
