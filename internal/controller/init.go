package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1 "yellowtang/api/v1"
)

// 初始化: 一主多从
// 创建主库和从库 service
// 创建主库和从库 configmap
// 创建主库和从库的 pod
// 创建主从关系
func (r *YellowTangReconciler) init(ctx context.Context, tang *appsv1.YellowTang) error {
	logger := log.FromContext(ctx)
	logger.Info("开始初始化集群...")

	// 校验副本数量
	replicas := tang.Spec.Replicas
	if replicas < 1 {
		return fmt.Errorf("invalid replica count: %d", replicas)
	}

	// 创建 svc
	if _, err := r.getorCreateService(tang.Spec.MasterServiceName, "master", ctx, tang); err != nil {
		logger.Error(err, "创建 master svc 失败")
		return fmt.Errorf("failed to create master service: %v", err)
	}
	if _, err := r.getorCreateService(tang.Spec.SlaveServiceName, "slave", ctx, tang); err != nil {
		logger.Error(err, "创建 slave svc 失败")
		return fmt.Errorf("failed to create slave service: %v", err)
	}

	// 创建 cm
	for i := int32(1); i <= replicas; i++ {
		serverId := int(i)
		configMapName := fmt.Sprintf("mysql-%02d", i)
		if _, err := r.getorCreatConfigMap(configMapName, serverId, ctx, tang); err != nil {
			return fmt.Errorf("failed to create configmap %s: %v", configMapName, err)
		}
	}

	// 创建 pvc
	for i := int32(1); i <= replicas; i++ {
		pvcName := fmt.Sprintf("mysql-%02d", i)
		if _, err := r.getorCreatePVC(pvcName, ctx, tang); err != nil {
			return fmt.Errorf("failed to create pvc %s: %v", pvcName, err)
		}
	}

	// 创建 Pod
	for i := int32(1); i <= replicas; i++ {
		podName := fmt.Sprintf("mysql-%02d", i)
		pvcName := fmt.Sprintf("mysql-%02d", i)
		configmapName := fmt.Sprintf("mysql-%02d", i)

		if _, err := r.createPod(podName, pvcName, configmapName, ctx, tang); err != nil {
			return fmt.Errorf("failed to create pod %s: %v", podName, err)
		}
	}
	return nil
}
