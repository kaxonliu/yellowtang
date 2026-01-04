package controller

import (
	"context"
	"fmt"
	"strings"
	appsv1 "yellowtang/api/v1"

	corev1 "k8s.io/api/core/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// 通过查看 master-service 关联的 endpoint 是否为空来判断主库是否挂掉
// 返回主库是否OK,主库pod的名字
func (r *YellowTangReconciler) checkMasterStatus(ctx context.Context, tang *appsv1.YellowTang) (bool, string, error) {
	// 获取 master-service 关联的 Endpoints
	endpoints := corev1.Endpoints{}
	endpointKey := client.ObjectKey{Name: tang.Spec.MasterServiceName, Namespace: tang.Namespace}
	if err := r.Get(ctx, endpointKey, &endpoints); err != nil {
		return false, "", err
	}

	// 检查 endpoints 中是否有 addresses，若为空则表示主库挂掉
	if len(endpoints.Subsets) == 0 || len(endpoints.Subsets[0].Addresses) == 0 {
		return false, "", nil
	}

	masterPodName := endpoints.Subsets[0].Addresses[0].TargetRef.Name
	return true, masterPodName, nil
}

// 处理出库挂掉的情况
// 当主库挂掉时，需要选举新的主库并重新配置主从关系：
func (r *YellowTangReconciler) handleMasterFailure(ctx context.Context, tang *appsv1.YellowTang) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("开始处理出库挂掉的情况...")

	// 选举新的主库（假设选举逻辑已经实现）
	newMasterName, remainingSlaves, err := r.electNewMaster(ctx, tang)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 重新配置主从关系
	if err := r.setupMasterSlaveReplication(ctx, newMasterName, remainingSlaves, tang); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// 检查所有从pod的主从状态，返回所有异常的从pod构成的数组
func (r *YellowTangReconciler) checkSlaveStatus(masterPodName string, ctx context.Context, tang *appsv1.YellowTang) ([]corev1.Pod, []corev1.Pod, error) {
	// 判断所有从pod名字的主从状态
	// 判断依据是判断sql线程与io线程同时为yes代表成功否则失败

	log := log.FromContext(ctx)
	log.Info("开始检查所有从库的主从状态...")

	failedSlavePodList := []corev1.Pod{}

	// 筛选出来所有的从pod
	allSlavePodList := []corev1.Pod{}

	allPodList, err := r.getPodByLabels(map[string]string{"tang": "true", "app": "mysql"}, ctx, tang)
	if err != nil {
		return allSlavePodList, failedSlavePodList, fmt.Errorf("failed to get all pod %v", err)
	}

	_allPodNameList := []string{}
	_allSlavePodNameList := []string{}
	for _, pod := range allPodList {
		_allPodNameList = append(_allPodNameList, pod.Name)
		if pod.Name != masterPodName {
			allSlavePodList = append(allSlavePodList, pod)
			_allSlavePodNameList = append(_allSlavePodNameList, pod.Name)
		}
	}
	log.Info("所有pod", "Pod", _allPodNameList)
	log.Info("所有从库pod", "Pod", _allSlavePodNameList)

	// 筛选出来主从状态异常的从pod
	// 准备 SQL 查询命令
	sqlQuery := fmt.Sprintf("mysql -uroot -p%s -e \"SHOW SLAVE STATUS \\G\"", MySQLPassword)

	for _, pod := range allSlavePodList {
		// 执行 SQL 查询
		output, err := r.execCommandOnPod(&pod, sqlQuery)
		if err != nil {
			log.Info("从库状态检测失败", "Pod", pod.Name, "错误", err)
			failedSlavePodList = append(failedSlavePodList, pod)
			continue
		}

		// 解析 SQL 查询结果
		sqlThread := strings.Contains(output, "Slave_SQL_Running: Yes")
		ioThread := strings.Contains(output, "Slave_IO_Running: Yes")

		if !(sqlThread && ioThread) {
			log.Info("从库状态检测失败", "Pod", pod.Name, "错误", "从库状态的返回字段匹配失败")
			failedSlavePodList = append(failedSlavePodList, pod)
		}
	}

	_failedSlavePodNameList := []string{}
	for _, pod := range failedSlavePodList {
		_failedSlavePodNameList = append(_failedSlavePodNameList, pod.Name)
	}
	log.Info("主从状态检查完成", "主库", masterPodName, "状态失败的从库", _failedSlavePodNameList)

	// 返回主库名称和所有主从状态异常的从库名称
	return allSlavePodList, failedSlavePodList, nil
}

// 检测主从状态
// 如果状态检查失败则返回错误，会重新排队调谐
// 如果检测后发现主库挂了，则重新选举主库
// 如果检测后发现主库没问题，那检测从库的状态并做主从设置
func (r *YellowTangReconciler) checkCluster(ctx context.Context, tang *appsv1.YellowTang) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("开始集群主从状态检测...")

	// 检查主库是否挂掉
	masterAlive, masterPodName, err := r.checkMasterStatus(ctx, tang)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !masterAlive {
		// 主库挂了
		if result, err := r.handleMasterFailure(ctx, tang); err != nil {
			// 如果处理主库故障时出现错误，则返回错误并重新排队调谐
			return result, err
		}
	} else {
		// 主库OK,检查从库
		allSlavePodList, failedSlavePodList, err := r.checkSlaveStatus(masterPodName, ctx, tang)
		if err != nil {
			return ctrl.Result{}, err
		}

		// 重新配置失败的从库
		failedSlavePodNameList := []string{}
		for _, pod := range failedSlavePodList {
			failedSlavePodNameList = append(failedSlavePodNameList, pod.Name)
		}
		// 避免重复设置主库
		if len(failedSlavePodNameList) >= 1 {
			if err := r.setupMasterSlaveReplication(ctx, masterPodName, failedSlavePodNameList, tang); err != nil {
				return ctrl.Result{}, err
			}
		}
		// 确保所有的从Pod都有标签 role=slave
		for _, pod := range allSlavePodList {
			r.labelPod(&pod, "slave", ctx, tang)
		}
	}

	return ctrl.Result{}, nil
}
