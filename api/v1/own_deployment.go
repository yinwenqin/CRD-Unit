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

type OwnDeployment struct {
	Spec appsv1.DeploymentSpec `json:"spec"`
}

func (ownDeployment *OwnDeployment) MakeOwnResource(instance *Unit, logger logr.Logger,
	scheme *runtime.Scheme) (interface{}, error) {

	// new a deployment object
	deployment := &appsv1.Deployment{
		// metadata field inherited from owner Unit
		//ObjectMeta: instance.ObjectMeta,
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace:instance.Namespace, Labels: instance.Labels},
		Spec:       ownDeployment.Spec,
	}

	//deployment.Spec.Template.Labels
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
	templateEnvs := deployment.Spec.Template.Spec.Containers[0].Env
	for index := range templateEnvs {
		if templateEnvs[index].Name != "POD_NAME" && templateEnvs[index].Name != "APPNAME" {
			specEnvs = append(specEnvs, templateEnvs[index])
		}
	}

	deployment.Spec.Template.Spec.Containers[0].Env = append(specEnvs, customizeEnvs...)

	// add ControllerReference for deployment，the owner is Unit object
	if err := controllerutil.SetControllerReference(instance, deployment, scheme); err != nil {
		msg := fmt.Sprintf("set controllerReference for Deployment %s/%s failed", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return nil, err
	}
	return deployment, nil
}

// Check if the Deployment already exists
func (ownDeployment *OwnDeployment) OwnResourceExist(instance *Unit, client client.Client,
	logger logr.Logger) (bool, interface{}, error) {

	found := &appsv1.Deployment{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil, nil
		}
		msg := fmt.Sprintf("Deployment %s/%s found, but with error:  \n", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return true, found, err
	}
	return true, found, nil
}

func (ownDeployment *OwnDeployment) UpdateOwnResourceStatus(instance *Unit, client client.Client,
	logger logr.Logger) (*Unit, error) {

	// 获取deployment的状态
	found := &appsv1.Deployment{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		msg := fmt.Sprintf("get Unit %s/%s own deployment status error", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return instance, err
	}

	// 将deployment的状态更新到Unit.status.deployment中
	instance.Status.BaseDeployment = found.Status
	instance.Status.LastUpdateTime = metav1.Now()

	return instance, nil
}

// apply this own resource, create or update
func (ownDeployment *OwnDeployment) ApplyOwnResource(instance *Unit, client client.Client,
	logger logr.Logger, scheme *runtime.Scheme) error {
	// make deployment object
	deployment, err := ownDeployment.MakeOwnResource(instance, logger, scheme)
	if err != nil {
		return err
	}
	newDeployment := deployment.(*appsv1.Deployment)


	// assert if deployment already exist
	exist, found, err := ownDeployment.OwnResourceExist(instance, client, logger)
	if err != nil {
		return err
	}

	// apply the deployment object just make
	if !exist {
		// if deployment not exist，then create it
		msg := fmt.Sprintf("Deployment %s/%s not found, create it!", newDeployment.Namespace, newDeployment.Name)
		logger.Info(msg)
		if err := client.Create(context.TODO(), newDeployment); err != nil {
			return err
		}
		return nil

	} else {
		foundDeployment := found.(*appsv1.Deployment)
		// if deployment exist with change，then try to update it
		if !reflect.DeepEqual(newDeployment.Spec, foundDeployment.Spec) {
			msg := fmt.Sprintf("Updating Deployment %s/%s", newDeployment.Namespace, newDeployment.Name)
			logger.Info(msg)
			return client.Update(context.TODO(), newDeployment)
		}
		return nil
	}
}
