package controller

import (
	"context"
	appsv1 "yellowtang/api/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *YellowTangReconciler) checkCluster(ctx context.Context, tang *appsv1.YellowTang) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("开始检测集群主从状态是否正常")

	return ctrl.Result{}, nil
}
