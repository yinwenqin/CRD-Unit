/*


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

package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	customv1 "Unit/api/v1"
)

type OwnResource interface {
	// 根据Unit的指定，生成相应的各类own build-in资源对象，用作创建或更新
	MakeOwnResource(instance *customv1.Unit, logger logr.Logger, scheme *runtime.Scheme) (interface{}, error)

	// 判断此资源是否已存在
	OwnResourceExist(instance *customv1.Unit, client client.Client, logger logr.Logger) (bool, interface{}, error)

	// 获取Unit对应的own build-in资源的状态，用来填充Unit的status字段
	UpdateOwnResourceStatus(instance *customv1.Unit, client client.Client, logger logr.Logger) (*customv1.Unit, error)

	// 创建/更新 Unit对应的own build-in资源
	ApplyOwnResource(instance *customv1.Unit, client client.Client, logger logr.Logger, scheme *runtime.Scheme) error
}

// UnitReconciler reconciles a Unit object
type UnitReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=custom.my.crd.com,resources=units,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=custom.my.crd.com,resources=units/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=statefulSet,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployment,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=service,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=endpoint,verbs=get
// +kubebuilder:rbac:groups=core,resources=persistentVolumeClaimStatus,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups=extensions,resources=ingress,verbs=get;update;patch;delete

func (r *UnitReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	//_ = context.Background()
	ctx := context.Background()
	_ = r.Log.WithValues("unit", req.NamespacedName)

	// your logic here

	// panic recovery
	defer func() {
		if rec := recover(); r != nil {
			switch x := rec.(type) {
			case error:
				r.Log.Error(x, "Reconcile error")
			}
		}
	}()

	// 1. Get操作,获取 Unit object
	instance := &customv1.Unit{}

	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
	}

	// 2. 删除操作
	// 如果资源对象被直接删除，就无法再读取任何被删除对象的信息，这就会导致后续的清理工作因为信息不足无法进行，Finalizer字段设计来处理这种情况：
	// 2.1 当资源对象 Finalizer字段不为空时，delete操作就会变成update操作，即为对象加上deletionTimestamp时间戳
	// 2.2 当 当前时间在deletionTimestamp时间之后，且Finalizer已清空(视为清理后续的任务已处理完成)的情况下，就会gc此对象了

	myFinalizerName := "storage.finalizers.tutorial.kubebuilder.io"
	//orphanFinalizerName := "orphan"

	// 2.1 DeletionTimestamp 时间戳为空，代表着当前对象不处于被删除的状态，为了开启Finalizer机制，先给它加上一段Finalizers，内容随机非空字符串即可
	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(instance.ObjectMeta.Finalizers, myFinalizerName) {
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, myFinalizerName)
			if err := r.Update(ctx, instance); err != nil {
				r.Log.Error(err, "Add Finalizers error", instance.Namespace, instance.Name)
				return ctrl.Result{}, err
			}
		}
	} else {
		// 2.2  DeletionTimestamp不为空，说明对象已经开始进入删除状态了，执行自己的删除步骤后续的逻辑，并清除掉自己的finalizer字段，等待自动gc
		if containsString(instance.ObjectMeta.Finalizers, myFinalizerName) {

			// 在删除owner resource之前，先执行自定义的预删除步骤，例如删除owner resource
			if err := r.PreDelete(instance); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, err
			}

			// 移出掉自定义的Finalizers，这样当Finalizers为空时，gc就会正式开始了
			instance.ObjectMeta.Finalizers = removeString(instance.ObjectMeta.Finalizers, myFinalizerName)
			if err := r.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// 3. 创建或更新操作
	// 3.1 根据Unit.spec 生成Unit关联的所有own build-in resource
	ownResources, err := r.getOwnResources(instance)
	if err != nil {
		msg := fmt.Sprintf("%s %s Reconciler.getOwnResource() function error", instance.Namespace, instance.Name)
		r.Log.Error(err, msg)
		return ctrl.Result{}, err
	}

	// 3.2 判断各own resource 是否存在，不存在则创建，存在则判断spec是否有变化，有变化则更新
	success := true
	for _, ownResource := range ownResources {
		if err = ownResource.ApplyOwnResource(instance, r.Client, r.Log, r.Scheme); err != nil {
			success = false
		}
	}

	// 4. update Unit.status
	// 4.1 更新实例Unit.Status 字段
	updateInstance := instance.DeepCopy()
	for _, ownResource := range ownResources {
		updateInstance, err = ownResource.UpdateOwnResourceStatus(updateInstance, r.Client, r.Log)
		if err != nil {
			//fmt.Println("update Unit ownresource status error:", err)
			success = false
		}
	}

	// 4.2 apply update to apiServer if status changed
	if updateInstance != nil && !reflect.DeepEqual(updateInstance.Status, instance.Status) {
		if err := r.Status().Update(context.Background(), updateInstance); err != nil {
			r.Log.Error(err, "unable to update Unit status")
		}
	}

	// 5. 记录结果
	if !success {
		msg := fmt.Sprintf("Reconciler Unit %s/%s failed ", instance.Namespace, instance.Name)
		r.Log.Error(err, msg)
		return ctrl.Result{}, err
	} else {
		msg := fmt.Sprintf("Reconcile Unit %s/%s success", instance.Namespace, instance.Name)
		r.Log.Info(msg)
		return ctrl.Result{}, nil
	}
}

func (r *UnitReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&customv1.Unit{}).
		Complete(r)
}

// Unit pre delete logic
func (r *UnitReconciler) PreDelete(instance *customv1.Unit) error {
	// 特别说明，own resource加上了ControllerReference之后，owner resource gc删除前，会先自动删除它的所有
	// own resources，因此绑定ControllerReference后无需再特别处理删除own resource。

	// 这里留空出来，是为了如果有自定义的pre delete逻辑的需要，可在这里实现。

	return nil
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

// 根据Unit.Spec生成其所有的own resource
func (r *UnitReconciler) getOwnResources(instance *customv1.Unit) ([]OwnResource, error) {
	var ownResources []OwnResource

	// Deployment 和StatefulSet 二者只能存在其一。由于可以动态选择，所以ownDeployment或ownStatefulSet在后端生成，不由前端指定
	if instance.Spec.Category == "Deployment" {
		ownDeployment := customv1.OwnDeployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: instance.Spec.Replicas,
				Selector: instance.Spec.Selector,
				Template: instance.Spec.Template,
			},
		}
		ownDeployment.Spec.Template.Labels = instance.Spec.Selector.MatchLabels
		ownResources = append(ownResources, &ownDeployment)

	} else {
		ownStatefulSet := &customv1.OwnStatefulSet{
			Spec: appsv1.StatefulSetSpec{
				Replicas:    instance.Spec.Replicas,
				Selector:    instance.Spec.Selector,
				Template:    instance.Spec.Template,
				ServiceName: instance.Name,
			},
		}

		ownResources = append(ownResources, ownStatefulSet)
	}

	// 将关联的资源(svc/ing/pvc)加入ownResources中
	if instance.Spec.RelationResource.Service != nil {
		ownResources = append(ownResources, instance.Spec.RelationResource.Service)
	}
	if instance.Spec.RelationResource.Ingress != nil {
		ownResources = append(ownResources, instance.Spec.RelationResource.Ingress)
	}
	if instance.Spec.RelationResource.PVC != nil {
		ownResources = append(ownResources, instance.Spec.RelationResource.PVC)
	}
	return ownResources, nil
}
