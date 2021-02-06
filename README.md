# CRD-Unit

## 前言

基于kubebuilder构建的CRD: **Unit**，代码仅供参考，重在CRD设计思路。

项目使用Kubebuilder脚手架实现，Unit的最终实现效果和Kubebuilder使用方法，请结合gitbook食用:

 [**《Kubebuilder v2使用指南》**](https://blog.upweto.top/gitbooks/kubebuilder/)



## 现状分析

通常一个运行服务(姑且这么称呼)，使用一系列独立原子性的build-in类型资源进行组合，来保障运行和提供服务，例如，最常用的组合有：StatefulSet/Deployment/Ingress/Service/Ingress这几种资源的按需组合，如下图：

<img src="http://mycloudn.kokoerp.com/20200521172548.jpg" style="zoom:50%;" />

这些资源类型每一种都是可选项，根据使用需求的不同，来灵活(弱绑定？)进行组合。

例如：

- 非web服务不需要Ingress资源
- 自发现和注册的应用不需要Service
- 无状态的应用选用Deployment，有状态的应用选用StatefulSet
- 有的应用无需持久存储，有的应用需求PVC来实现持久存储

**按需组合，不一而同**

这样的弱绑定关系在管理上不够友好，每种资源的增删改查等逻辑，需要分而治之。



## 设计目标

想要实现的CRD目标是：**每一个运行服务，所用到的的各类资源，在一个声明文件中将它们绑定在一起，进行统一原子性的生命周期管理**

由此，我将这个CRD命名为"Unit"，这意味着，StatefulSet/Deployment/Ingress/Service/Ingress这些可选的build-in资源，由原本松散组合构成一个运行服务的模式，变为集合在一个单元里。在上层，用户可以在一个声明文件中定义服务所需的各类资源组合，部署和维护一个服务时，仅需一对一地维护一个Unit CRD资源对象；在下层，CRD控制器按照Unit声明，实现对各个关联子资源进行统一管理。如下图：

<img src="http://mycloudn.kokoerp.com/20200522103739.jpg" style="zoom:35%;" />

