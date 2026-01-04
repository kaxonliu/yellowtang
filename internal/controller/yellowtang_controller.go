/*
Copyright 2026 kaxonliu.

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

package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1 "yellowtang/api/v1"
)

const (
	MysqlClusterKind       = "YellowTang"
	MysqlClusterAPIVersion = "apps.kaxonliu.com/v1"
	MySQLPassword          = "password"           // Hardcoded MySQL password
	KubeConfigPath         = "/root/.kube/config" // Hardcoded kubeconfig path
)

// YellowTangReconciler reconciles a YellowTang object
type YellowTangReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger // 日志记录器
}

// +kubebuilder:rbac:groups=apps.kaxonliu.com,resources=yellowtangs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kaxonliu.com,resources=yellowtangs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kaxonliu.com,resources=yellowtangs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the YellowTang object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *YellowTangReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("调谐函数触发执行", "req", req)

	var tang appsv1.YellowTang

	// 返回给客户端的错误提示
	if err := r.Get(ctx, req.NamespacedName, &tang); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 检查集群是否完成初始化
	// 初始化完成则进入检测逻辑
	// 未完成初始化则开始初始化集群
	if _, ok := tang.Annotations["initialized"]; !ok {
		// 初始化集群
		if err := r.init(ctx, &tang); err == nil {
			logger.Info("集群初始化成功")
		} else {
			logger.Error(err, "集群初始化失败")
			return ctrl.Result{}, err
		}

		// 标记集群完成初始化
		if tang.Annotations == nil {
			tang.Annotations = map[string]string{}
		}

		tang.Annotations["initialized"] = "true"
		if err := r.Update(ctx, &tang); err != nil {
			return ctrl.Result{}, err
		}

	} else {
		// 检测副本数
		if result, err := r.checkReplicas(ctx, &tang); err != nil {
			return result, err
		}

		//检测主从状态
		if result, err := r.checkCluster(ctx, &tang); err != nil {
			return result, err
		}

	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *YellowTangReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// 增加：Owns(&v1.Pod{})
	// 确保 pod 资源发生变动时触发调谐函数的执行
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.YellowTang{}).
		Owns(&corev1.Pod{}).
		WithOptions(controller.Options{
			// 增加重试次数
			MaxConcurrentReconciles: 1,
			// 调整事件处理
			RateLimiter: workqueue.NewItemExponentialFailureRateLimiter(
				time.Second, 10*time.Second),
		}).
		Complete(r)
}
