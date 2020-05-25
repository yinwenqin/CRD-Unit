package v1

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// pvc声明信息
type OwnPVC struct {
	Spec v1.PersistentVolumeClaimSpec ` json:"spec"`
}

func (ownPVC *OwnPVC) MakeOwnResource(instance *Unit, logger logr.Logger,
	scheme *runtime.Scheme) (interface{}, error) {

	// new a PVC object
	pvc := &v1.PersistentVolumeClaim{
		// metadata field inherited from owner Unit
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace, Labels: instance.Labels},
		Spec:       ownPVC.Spec,
	}

	// add ControllerReference for sts，the owner is Unit object
	if err := controllerutil.SetControllerReference(instance, pvc, scheme); err != nil {
		msg := fmt.Sprintf("set controllerReference for PVC %s/%s failed", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return nil, err
	}

	return pvc, nil
}

// Check if the ownPVC already exists
func (ownPVC *OwnPVC) OwnResourceExist(instance *Unit, client client.Client,
	logger logr.Logger) (bool, interface{}, error) {

	found := &v1.PersistentVolumeClaim{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil, nil
		}

		msg := fmt.Sprintf("PVC %s/%s found, but with error", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return true, found, err
	}
	return true, found, nil
}

func (ownPVC *OwnPVC) UpdateOwnResourceStatus(instance *Unit, client client.Client,
	logger logr.Logger) (*Unit, error) {

	found := &v1.PersistentVolumeClaim{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		return instance, err
	}

	instance.Status.RelationResourceStatus.PVC = found.Status
	instance.Status.LastUpdateTime = metav1.Now()

	return instance, nil
}

// apply this own resource, create or update
func (ownPVC *OwnPVC) ApplyOwnResource(instance *Unit, client client.Client,
	logger logr.Logger, scheme *runtime.Scheme) error {

	// assert if PVC exist
	exist, found, err := ownPVC.OwnResourceExist(instance, client, logger)
	if err != nil {
		return err
	}

	// make PVC object
	pvc, err := ownPVC.MakeOwnResource(instance, logger, scheme)
	if err != nil {
		return err
	}
	newPVC := pvc.(*v1.PersistentVolumeClaim)

	// apply the PVC object just make
	if !exist {
		// if PVC not exist，then create it
		msg := fmt.Sprintf("PVC %s/%s not found, create it!", newPVC.Namespace, newPVC.Name)
		logger.Info(msg)

		if err := client.Create(context.TODO(), newPVC); err != nil {
			return err
		}
		return nil
	} else {
		foundPVC := found.(*v1.PersistentVolumeClaim)
		// if PVC exist with change，then try to update it
		if !reflect.DeepEqual(newPVC.Spec, foundPVC.Spec) {
			msg := fmt.Sprintf("Updating PVC %s/%s", newPVC.Namespace, newPVC.Name)
			logger.Info(msg)
			return client.Update(context.TODO(), newPVC)
		}
		return nil
	}
}
