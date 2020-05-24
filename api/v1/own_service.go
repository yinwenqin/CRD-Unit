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
	"k8s.io/apimachinery/pkg/util/intstr"
	"net"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"time"
)

// svc的端口映射关系
type ServicePort struct {
	Name       string             `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	Protocol   string             `json:"protocol,omitempty" protobuf:"bytes,2,opt,name=protocol,casttype=Protocol"`
	Port       int32              `json:"port" protobuf:"varint,3,opt,name=port"`
	TargetPort intstr.IntOrString `json:"targetPort,omitempty" protobuf:"bytes,4,opt,name=targetPort"`
	NodePort   int32              `json:"nodePort,omitempty" protobuf:"varint,5,opt,name=nodePort"`
}

type OwnService struct {
	Ports     []v1.ServicePort `json:"ports,omitempty" patchStrategy:"merge" patchMergeKey:"port" protobuf:"bytes,1,rep,name=ports"`
	ClusterIP string           `json:"clusterIP,omitempty" protobuf:"bytes,3,opt,name=clusterIP"`
}

type ServicePortStatus struct {
	v1.ServicePort `json:"servicePort,omitempty"`
	// 检查此端口连通性
	Health bool `json:"health,omitempty"`
}

type UnitRelationServiceStatus struct {
	Type            v1.ServiceType      `json:"type,omitempty"`
	ClusterIP       string              `json:"clusterIP,omitempty"`
	Ports           []ServicePortStatus `json:"ports,omitempty"`
	SessionAffinity v1.ServiceAffinity  `json:"sessionAffinity,omitempty"`
}

type UnitRelationEndpointStatus struct {
	PodName  string `json:"podName"`
	PodIP    string `json:"podIP"`
	NodeName string `json:"nodeName"`
}

func (ownService *OwnService) MakeOwnResource(instance *Unit, logger logr.Logger,
	scheme *runtime.Scheme) (interface{}, error) {

	// new a Service object
	svc := &v1.Service{
		// metadata field inherited from owner Unit
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace:instance.Namespace, Labels: instance.Labels},
		Spec: v1.ServiceSpec{
			Ports: ownService.Ports,
			Type:  v1.ServiceTypeClusterIP,
		},
	}

	if ownService.ClusterIP != "" {
		svc.Spec.ClusterIP = ownService.ClusterIP
	}

	// add selector
	labelMap := make(map[string]string, 1)
	labelMap["app"] = instance.Name
	svc.Spec.Selector = labelMap


	//svc.Spec.Selector =
	// add ControllerReference for sts，the owner is Unit object
	if err := controllerutil.SetControllerReference(instance, svc, scheme); err != nil {
		msg := fmt.Sprintf("set controllerReference for Service %s/%s failed", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return nil, err
	}

	return svc, nil
}

// Check if the ownService already exists
func (ownService *OwnService) OwnResourceExist(instance *Unit, client client.Client,
	logger logr.Logger) (bool, interface{}, error) {

	found := &v1.Service{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil, nil
		}
		msg := fmt.Sprintf("Service %s/%s found, but with error: %s  \n", instance.Namespace, instance.Name)
		logger.Error(err, msg)
		return true, found, err
	}
	return true, found, nil
}

// 更新own Service的status，以及own Service对应的Endpoint状态也一起在这里处理
func (ownService *OwnService) UpdateOwnResourceStatus(instance *Unit, client client.Client,
	logger logr.Logger) (*Unit, error) {

	// 更新Service status
	found := &v1.Service{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, found)
	if err != nil {
		return instance, err
	}

	var portsStatus []ServicePortStatus
	for _, port := range found.Spec.Ports {
		// 对 service的每一个端口进行健康检查，并在status中加上健康状态的字段
		health := true
		checkPort := port.Port
		addr := found.Spec.ClusterIP
		sock := fmt.Sprintf("%s:%d", addr, checkPort)
		proto := string(port.Protocol)
		_, err := net.DialTimeout(proto, sock, time.Duration(100)*time.Millisecond)
		if err != nil {
			health = false
		}

		portStatus := ServicePortStatus{
			ServicePort: port,
			Health:      health,
		}
		portsStatus = append(portsStatus, portStatus)
	}

	serviceStatus := UnitRelationServiceStatus{
		Type:      found.Spec.Type,
		ClusterIP: found.Spec.ClusterIP,
		Ports:     portsStatus,
	}
	instance.Status.RelationResourceStatus.Service = serviceStatus

	// 更新Endpoint status
	foundEndpoint := &v1.Endpoints{}
	err = client.Get(context.TODO(), types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, foundEndpoint)
	if err != nil {
		return instance, err
	}

	if foundEndpoint.Subsets != nil && foundEndpoint.Subsets[0].Addresses != nil {
		var endpointsStatus []UnitRelationEndpointStatus
		for _, ep := range foundEndpoint.Subsets[0].Addresses {
			endpointStatus := UnitRelationEndpointStatus{
				PodName:  ep.Hostname,
				PodIP:    ep.IP,
				NodeName: *ep.NodeName,
			}
			endpointsStatus = append(endpointsStatus, endpointStatus)
		}
		instance.Status.RelationResourceStatus.Endpoint = endpointsStatus
	}

	// update LastUpdateTime
	instance.Status.LastUpdateTime = metav1.Now()

	return instance, nil
}

// apply this own resource, create or update
func (ownService *OwnService) ApplyOwnResource(instance *Unit, client client.Client,
	logger logr.Logger, scheme *runtime.Scheme) error {

	// assert if Service exist
	exist, found, err := ownService.OwnResourceExist(instance, client, logger)
	if err != nil {
		return err
	}

	// make Service object
	sts, err := ownService.MakeOwnResource(instance, logger, scheme)
	if err != nil {
		return err
	}
	newService := sts.(*v1.Service)

	// apply the Service object just make
	if !exist {
		// if Service not exist，then create it
		msg := fmt.Sprintf("Service %s/%s not found, create it!", newService.Namespace, newService.Name)
		logger.Info(msg)
		return client.Create(context.TODO(), newService)
	} else {
		foundService := found.(*v1.Service)

		// 这里有个坑，svc在创建前可能未指定clusterIP，那么svc创建后，会自动指定clusterIP并修改spec.clusterIP字段，因此这里要补上。SessionAffinity同理
		newService.Spec.ClusterIP = foundService.Spec.ClusterIP
		newService.Spec.SessionAffinity = foundService.Spec.SessionAffinity

		// if Service exist with change，then try to update it
		if !reflect.DeepEqual(newService.Spec, foundService.Spec) {
			msg := fmt.Sprintf("Updating Service %s/%s", newService.Namespace, newService.Name)
			logger.Info(msg)
			return client.Update(context.TODO(), newService)
		}
		return nil
	}
}
