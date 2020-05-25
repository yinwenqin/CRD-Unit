package v1

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type OwnStatefulSet struct {
	Spec appsv1.StatefulSetSpec
}

func (ownStatefulSet *OwnStatefulSet) MakeOwnResource(instance *Unit, logger logr.Logger,
	scheme *runtime.Scheme) (interface{}, error) {

	// new a StatefulSet object
	sts := &appsv1.StatefulSet{
		// metadata field inherited from owner Unit
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace, Labels: instance.Labels},
		Spec:       ownStatefulSet.Spec,
	}

	// add some customize envs, ignore this step if you don't need it
	customizeEnvs := []v1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.name",
				},
			},
		},
		{
			Name:  "APPNAME",
			Value: instance.Name,
		},
	}

	var specEnvs []v1.EnvVar
	templateEnvs := sts.Spec.Template.Spec.Containers[0].Env
	for index := range templateEnvs {
		if templateEnvs[index].Name != "POD_NAME" && templateEnvs[index].Name != "APPNAME" {
			specEnvs = append(specEnvs, templateEnvs[index])
		}
	}

	sts.Spec.Template.Spec.Containers[0].Env = append(specEnvs, customizeEnvs...)

	// add ControllerReference for sts，the owner is Unit object
	if err := controllerutil.SetControllerReference(instance, sts, scheme); err != nil {
		msg := fmt.Sprintf("set controllerReference for StatefulSet %s/%s failed", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return nil, err
	}

	return sts, nil
}

// Check if the StatefulSet already exists
func (ownStatefulSet *OwnStatefulSet) OwnResourceExist(instance *Unit, client client.Client,
	logger logr.Logger) (bool, interface{}, error) {

	found := &appsv1.StatefulSet{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil, nil
		}
		msg := fmt.Sprintf("StatefulSet %s/%s found, but with error", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return true, found, err
	}
	return true, found, nil
}

func (ownStatefulSet *OwnStatefulSet) UpdateOwnResourceStatus(instance *Unit, client client.Client,
	logger logr.Logger) (*Unit, error) {

	found := &appsv1.StatefulSet{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		return instance, err
	}
	instance.Status.BaseStatefulSet = found.Status
	instance.Status.LastUpdateTime = metav1.Now()
	return instance, nil

	//if err := client.Status().Update(context.Background(), instance); err != nil {
	//	logger.Error(err, "unable to update Unit StatefulSet status")
	//	return instance, err
	//}

}

// apply this own resource, create or update
func (ownStatefulSet *OwnStatefulSet) ApplyOwnResource(instance *Unit, client client.Client,
	logger logr.Logger, scheme *runtime.Scheme) error {

	// assert if StatefulSet exist
	exist, found, err := ownStatefulSet.OwnResourceExist(instance, client, logger)
	if err != nil {
		return err
	}

	// make StatefulSet object
	sts, err := ownStatefulSet.MakeOwnResource(instance, logger, scheme)
	if err != nil {
		return err
	}
	newStatefulSet := sts.(*appsv1.StatefulSet)

	// apply the StatefulSet object just make
	if !exist {
		// if StatefulSet not exist，then create it
		msg := fmt.Sprintf("StatefulSet %s/%s not found, create it!", newStatefulSet.Namespace, newStatefulSet.Name)
		logger.Info(msg)
		if err := client.Create(context.TODO(), newStatefulSet); err != nil {
			return err
		}
		return nil

	} else {
		foundStatefulSet := found.(*appsv1.StatefulSet)

		// if StatefulSet exist with change，then try to update it
		if !reflect.DeepEqual(newStatefulSet.Spec, foundStatefulSet.Spec) {
			msg := fmt.Sprintf("Updating StatefulSet %s/%s", newStatefulSet.Namespace, newStatefulSet.Name)
			logger.Info(msg)
			return client.Update(context.TODO(), newStatefulSet)
		}
		return nil
	}
}
