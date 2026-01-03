package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1 "yellowtang/api/v1"
)

func (r *YellowTangReconciler) checkReplicas(ctx context.Context, tang *appsv1.YellowTang) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("开始检测副本数量是否满足预期")

	return ctrl.Result{}, nil
}
