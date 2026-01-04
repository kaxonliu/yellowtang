package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	appsv1 "yellowtang/api/v1"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// 选主逻辑：
/*
MHA数据一致性和选择
GTID模式：MHA会比较每个从库的GTID集合。选择包含主库GTID率高的的从库作为新的主库。
*/
func (r *YellowTangReconciler) electNewMaster(ctx context.Context, tang *appsv1.YellowTang) (string, []string, error) {
	logger := log.FromContext(ctx)
	logger.Info("开始选举新主...")

	labels := map[string]string{"tang": "true", "app": "mysql", "role": "slave"}
	slavePodList, err := r.getPodByLabels(labels, ctx, tang)
	if err != nil {
		return "", nil, fmt.Errorf("failed to list slave pods: %v", err)
	}
	_slavePodNameList := []string{}
	for _, pod := range slavePodList {
		_slavePodNameList = append(_slavePodNameList, pod.Name)
	}
	logger.Info("所有从pod", "Pod", _slavePodNameList)

	// 初始化选举变量
	var bestSlave *corev1.Pod
	var highestScore float64 = -1

	// 遍历所有从库 Pod
	for _, pod := range slavePodList {
		// 确保 Pod 健康
		if !isPodHealthy(pod) {
			continue
		}

		// 计算数据得分
		dataScore, err := r.getDataScore(ctx, &pod)
		if err != nil {
			return "", nil, fmt.Errorf("failed to get data score for pod %s: %v", pod.Name, err)
		}

		// 如果当前 Pod 的数据得分更高，则更新最佳 Pod
		if dataScore > highestScore {
			highestScore = dataScore
			bestSlave = &pod
		}
	}
	// 如果没有找到合适的从库，则返回错误
	if bestSlave == nil {
		return "", nil, fmt.Errorf("no suitable slave found to be promoted to master")
	}

	// 确定新的主库名称
	newMasterName := bestSlave.Name
	var remainingSlaves []string

	// 过滤掉新的主库，获取剩余的从库
	for _, pod := range slavePodList {
		if pod.Name != newMasterName {
			remainingSlaves = append(remainingSlaves, pod.Name)
		}
	}
	logger.Info("成功选举新主库", "新主库", newMasterName, "其余从库", remainingSlaves)
	return newMasterName, remainingSlaves, nil

}

// 计算数据得分的函数
func (r *YellowTangReconciler) getDataScore(ctx context.Context, pod *corev1.Pod) (float64, error) {
	// 获取主库的 GTID 集合快照
	masterGTIDSet := r.MasterGTIDSnapshot

	// 获取从库的 GTID 集合
	slaveGTIDSet, err := r.getSlaveGTIDSet(ctx, pod)
	if err != nil {
		return 0.0, fmt.Errorf("failed to get slave GTID set: %v", err)
	}

	// 计算 GTID 完整度得分
	gtidScore := r.calculateGTIDScore(masterGTIDSet, slaveGTIDSet)

	// 获取从库的数据量
	dataSize, err := r.getDataSize(ctx, pod)
	if err != nil {
		return 0.0, fmt.Errorf("failed to get data size: %v", err)
	}

	// 计算数据量得分
	dataScore := r.calculateDataScore(dataSize)

	// 合成最终得分
	finalScore := gtidScore + dataScore

	return finalScore, nil
}

// 获取主库的 GTID 集合
func (r *YellowTangReconciler) getMasterGTIDSet(ctx context.Context, pod *corev1.Pod) (string, error) {
	masterCommand := "mysql -uroot -p%s -e \"SHOW MASTER STATUS\\G\" | grep 'Executed_Gtid_Set:' | awk '{print $2}'"
	command := fmt.Sprintf(masterCommand, MySQLPassword)
	_, err := r.execCommandOnPod(pod, command)
	if err != nil {
		return "", err
	}
	return "", nil
}

// 获取从库的 GTID 集合
func (r *YellowTangReconciler) getSlaveGTIDSet(ctx context.Context, pod *corev1.Pod) (string, error) {
	slaveCommand := "mysql -uroot -p%s -e \"SHOW SLAVE STATUS\\G\" | grep 'Retrieved_Gtid_Set:' | awk '{print $2}'"
	command := fmt.Sprintf(slaveCommand, MySQLPassword)
	_, err := r.execCommandOnPod(pod, command)
	if err != nil {
		return "", err
	}
	return "", nil
}

// 计算 GTID 完整度得分
func (r *YellowTangReconciler) calculateGTIDScore(masterGTIDSet, slaveGTIDSet string) float64 {
	// 计算 GTID 完整度得分的逻辑
	// 例如，可以基于主库和从库的 GTID 集合的差异计算得分
	if masterGTIDSet == "" || slaveGTIDSet == "" {
		return 0.0
	}

	// 这里假设得分与从库的 GTID 集合包含主库的 GTID 集合的比例有关
	masterGTIDs := strings.Split(masterGTIDSet, ",")
	slaveGTIDs := strings.Split(slaveGTIDSet, ",")

	// 简单示例：计算从库包含的 GTID 数量
	count := 0
	for _, masterGTID := range masterGTIDs {
		for _, slaveGTID := range slaveGTIDs {
			if masterGTID == slaveGTID {
				count++
				break
			}
		}
	}

	// 得分：包含的 GTID 数量占主库 GTID 数量的比例
	return float64(count) / float64(len(masterGTIDs))
}

// 获取从库的数据量
func (r *YellowTangReconciler) getDataSize(ctx context.Context, pod *corev1.Pod) (int64, error) {
	// 获取 MySQL 数据目录的路径
	// 这里使用了一个假设的默认路径，我们采用的容器中固定目录就是这个
	dataDirPath := "/var/lib/mysql"

	// 使用 du 命令计算数据目录的大小
	dataSizeCommand := fmt.Sprintf("du -sb %s | awk '{print $1}'", dataDirPath)
	output, err := r.execCommandOnPod(pod, dataSizeCommand)
	if err != nil {
		return 0, err
	}

	// 解析数据大小
	dataSize, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil {
		return 0, err
	}

	return dataSize, nil
}

// 计算数据量得分
func (r *YellowTangReconciler) calculateDataScore(dataSize int64) float64 {
	// 计算数据量得分的逻辑
	// 这里简单地将数据大小作为得分值
	return float64(dataSize)
}
