package controller

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1 "yellowtang/api/v1"
)

func (r *YellowTangReconciler) checkReplicas(ctx context.Context, tang *appsv1.YellowTang) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("开始检测副本数量是否满足预期")

	selectLabels := map[string]string{"tang": "true", "app": "mysql"}
	actualPods, err := r.getPodByLabels(selectLabels, ctx, tang)
	if err != nil {
		return ctrl.Result{}, err
	}
	targetReplicas := tang.Spec.Replicas
	actualReplicas := len(actualPods)
	logger.Info("当前副本情况", "副本数", actualReplicas, "预期副本数", targetReplicas)

	if targetReplicas == int32(actualReplicas) {
		// 副本数一致,无需调节
		return ctrl.Result{}, nil
	}
	logger.Info("副本数与预期不符", "实际副本数", actualReplicas, "预期副本数", targetReplicas)

	// 创建缺失的副本
	targetReplicasNos := generateNumberRange(1, int(targetReplicas))
	actualReplicasNos := parsePodNos(actualPods)
	missingReplicasNos := getMissingReplicasNos(targetReplicasNos, actualReplicasNos)
	for _, podNo := range missingReplicasNos {

		podName := fmt.Sprintf("mysql-%s", podNo)
		pvcName := fmt.Sprintf("mysql-%s", podNo)
		configMapName := fmt.Sprintf("mysql-%s", podNo)

		// 如果 cm pvc pv 不存在则会新建
		// 如果存在，k8s 接口内部会做判断，不会重复创建
		serverId, _ := strconv.Atoi(podNo)
		if _, err := r.createConfigMap(configMapName, serverId, ctx, tang); err != nil {
			return ctrl.Result{}, err
		}
		if _, err := r.createPVC(pvcName, ctx, tang); err != nil {
			return ctrl.Result{}, err
		}

		// 创建 pod
		if _, err := r.createPod(podName, pvcName, configMapName, ctx, tang); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("创建缺失的 Pod", "PodName", podName)
	}
	return ctrl.Result{}, nil
}

func generateNumberRange(start, end int) []string {
	if end < start {
		return []string{}
	}

	result := make([]string, end-start+1)
	for i := start; i <= end; i++ {
		result[i-start] = strconv.Itoa(i)
	}
	return result
}

func parsePodNos(podList []corev1.Pod) []string {
	var podNos []string
	var podNamePattern = regexp.MustCompile(`mysql-(\d+)`)

	for _, pod := range podList {
		if matches := podNamePattern.FindStringSubmatch(pod.Name); len(matches) > 1 {
			podNos = append(podNos, matches[1])
		}
	}
	return podNos
}

func getMissingReplicasNos(a, b []string) []string {
	// 差集：在A中但不在B中的元素
	// 创建集合B
	setB := make(map[string]bool)
	for _, v := range b {
		setB[v] = true
	}

	// 查找在A中但不在B中的元素
	var diff []string
	for _, v := range a {
		if !setB[v] {
			diff = append(diff, v)
		}
	}
	return diff
}
