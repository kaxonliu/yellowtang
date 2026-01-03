package controller

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1 "yellowtang/api/v1"
)

func TrimLinesRegex(text string) string {
	// 去除每行开头的空格和制表符
	re := regexp.MustCompile(`(?m)^[ \t]+|[ \t]+$`)
	return re.ReplaceAllString(text, "")
}

func isPodHealthy(pod corev1.Pod) bool {
	// 实现健康检查逻辑，例如通过 Pod 的状态、容器状态等
	return pod.Status.Phase == corev1.PodRunning && len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].Ready
}

func (r *YellowTangReconciler) getService(serviceKey client.ObjectKey, ctx context.Context, tang *appsv1.YellowTang) (*corev1.Service, error) {
	service := corev1.Service{}
	if err := r.Get(ctx, serviceKey, &service); err != nil {
		return nil, err
	}
	return &service, nil

}

func (r *YellowTangReconciler) getorCreateService(name string, role string, ctx context.Context, tang *appsv1.YellowTang) (*corev1.Service, error) {
	logger := log.FromContext(ctx)
	serviceKey := client.ObjectKey{Namespace: tang.Namespace, Name: name}
	service, err := r.getService(serviceKey, ctx, tang)
	if err == nil {
		return service, nil
	}

	logger.Info("没有找到 svc", "svcName", name)
	if errors.IsNotFound(err) {
		logger.Info("准备创建 svc", "svcName", name)
		service, err := r.createService(name, role, ctx, tang)
		if err == nil {
			return service, nil
		}
	}
	return nil, err
}

func (r *YellowTangReconciler) createService(name string, role string, ctx context.Context, tang *appsv1.YellowTang) (*corev1.Service, error) {
	// 定义 OwnerReference
	logger := log.FromContext(ctx)
	ownerRef := metav1.OwnerReference{
		APIVersion: MysqlClusterAPIVersion,
		Kind:       MysqlClusterKind,
		Name:       tang.Name, // yellowtang-sample
		UID:        tang.UID,
		Controller: func(b bool) *bool { return &b }(true),
	}

	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tang.Namespace,
			Labels: map[string]string{
				"tang": "true",
				"app":  "mysql",
				"role": role,
			},
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"tang": "true",
				"app":  "mysql",
				"role": role,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       3306,
					TargetPort: intstr.FromInt(3306),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	if err := r.Create(ctx, &service); err != nil {
		logger.Info("创建 svc 失败", "svcName", name, "error", err)
		return nil, err
	}
	return &service, nil

}

func (r *YellowTangReconciler) getConfigMap(cmKey client.ObjectKey, ctx context.Context, tang *appsv1.YellowTang) (*corev1.ConfigMap, error) {
	cm := corev1.ConfigMap{}
	if err := r.Get(ctx, cmKey, &cm); err != nil {
		return nil, err
	}
	return &cm, nil

}

func (r *YellowTangReconciler) getorCreatConfigMap(name string, serverId int, ctx context.Context, tang *appsv1.YellowTang) (*corev1.ConfigMap, error) {
	cmKey := client.ObjectKey{Namespace: tang.Namespace, Name: name}
	cm, err := r.getConfigMap(cmKey, ctx, tang)
	if err == nil {
		return cm, nil
	}

	if errors.IsNotFound(err) {
		cm, err := r.createConfigMap(name, serverId, ctx, tang)
		if err == nil {
			return cm, nil
		}
	}
	return nil, err
}

func (r *YellowTangReconciler) createConfigMap(name string, serverId int, ctx context.Context, tang *appsv1.YellowTang) (*corev1.ConfigMap, error) {
	// 定义 OwnerReference
	ownerRef := metav1.OwnerReference{
		APIVersion: MysqlClusterAPIVersion,
		Kind:       MysqlClusterKind,
		Name:       tang.Name, // yellowtang-sample
		UID:        tang.UID,
		Controller: func(b bool) *bool { return &b }(true),
	}

	configMapData := fmt.Sprintf(`[mysqld]
        server-id=%d
        binlog_format=row
        log-bin=mysql-bin
        skip-name-resolve
        gtid-mode=on
        enforce-gtid-consistency=true
        log-slave-updates=1
        relay_log_purge=0
        # other configurations`, serverId)
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tang.Namespace,
			Labels: map[string]string{
				"tang": "true",
				"app":  "mysql",
			},
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			},
		},
		Data: map[string]string{
			"my.cnf": TrimLinesRegex(configMapData),
		},
	}
	if err := r.Create(ctx, &cm); err != nil {
		return nil, err
	}
	r.Log.Info("ConfigMap created successfully", "ConfigMap.Name", name)
	return &cm, nil

}

func (r *YellowTangReconciler) getPVC(pvcKey client.ObjectKey, ctx context.Context, tang *appsv1.YellowTang) (*corev1.PersistentVolumeClaim, error) {
	pvc := corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, pvcKey, &pvc); err != nil {
		return nil, err
	}
	return &pvc, nil

}

func (r *YellowTangReconciler) getorCreatePVC(name string, ctx context.Context, tang *appsv1.YellowTang) (*corev1.PersistentVolumeClaim, error) {
	pvcKey := client.ObjectKey{Namespace: tang.Namespace, Name: name}
	pvc, err := r.getPVC(pvcKey, ctx, tang)
	if err == nil {
		return pvc, nil
	}

	if errors.IsNotFound(err) {
		pvc, err := r.createPVC(name, ctx, tang)
		if err == nil {
			return pvc, nil
		}
	}
	return nil, err
}

func (r *YellowTangReconciler) createPVC(name string, ctx context.Context, tang *appsv1.YellowTang) (*corev1.PersistentVolumeClaim, error) {
	// 定义 OwnerReference
	ownerRef := metav1.OwnerReference{
		APIVersion: MysqlClusterAPIVersion,
		Kind:       MysqlClusterKind,
		Name:       tang.Name, // yellowtang-sample
		UID:        tang.UID,
		Controller: func(b bool) *bool { return &b }(true),
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tang.Namespace,
			Labels: map[string]string{
				"tang": "true",
				"app":  "mysql",
			},
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					v1.ResourceStorage: resource.MustParse(tang.Spec.Storage.Size),
				},
			},
			StorageClassName: &tang.Spec.Storage.StorageClassName,
		},
	}
	if err := r.Create(ctx, &pvc); err != nil {
		return nil, err
	}
	r.Log.Info("PVC created successfully", "PVC.Name", name)
	return &pvc, nil

}

func (r *YellowTangReconciler) createPod(podName, pvcName, configMapName string, ctx context.Context, tang *appsv1.YellowTang) (*corev1.Pod, error) {
	// 定义 OwnerReference
	ownerRef := metav1.OwnerReference{
		APIVersion: MysqlClusterAPIVersion,
		Kind:       MysqlClusterKind,
		Name:       tang.Name, // yellowtang-sample
		UID:        tang.UID,
		Controller: func(b bool) *bool { return &b }(true),
	}

	// 获取resources资源限制
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(tang.Spec.Resources.Requests.CPU),
			corev1.ResourceMemory: resource.MustParse(tang.Spec.Resources.Requests.Memory),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(tang.Spec.Resources.Limits.CPU),
			corev1.ResourceMemory: resource.MustParse(tang.Spec.Resources.Limits.Memory),
		},
	}

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: tang.Namespace,
			Labels: map[string]string{
				"tang": "true",
				"app":  "mysql",
			},
			OwnerReferences: []metav1.OwnerReference{
				ownerRef,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "mysql",
					Image: tang.Spec.Image,
					Env: []corev1.EnvVar{
						{
							Name:  "MYSQL_ROOT_PASSWORD",
							Value: MySQLPassword, // 设置 MySQL root 用户的密码
						},
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "mysql",
							ContainerPort: 3306,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "mysql-config",
							MountPath: "/etc/my.cnf",
							SubPath:   "my.cnf", // 使用SubPath挂载文件
						},
						{
							Name:      "mysql-data",
							MountPath: "/var/lib/mysql", // MySQL 数据目录
						},
					},
					Resources:      resources,
					ReadinessProbe: tang.Spec.ReadinessProbe,
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "mysql-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: configMapName,
							},
						},
					},
				},
				{
					Name: "mysql-data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName, // PVC 名称作为参数传入
						},
					},
				},
			},
		},
	}
	if err := r.Create(ctx, &pod); err != nil {
		r.Log.Error(err, "Failed to create Pod", "Pod.Name", podName)
		return nil, err
	}
	r.Log.Info("POD created successfully", "POD.Name", podName)

	// 新增：等待pod就绪的功能，否则会提前制作主从，会因为pod尚未就绪而导致大量失败
	podKey := client.ObjectKey{Namespace: tang.Namespace, Name: podName}

	start := time.Now()

	for {
		// 等待一段时间再检查 Pod 状态
		time.Sleep(5 * time.Second)

		// 获取 Pod 状态
		pod := &corev1.Pod{}
		if err := r.Get(ctx, podKey, pod); err != nil { // 在调用 r.Get 后，pod 变量将包含 Kubernetes 集群中该 Pod 的当前状态。
			r.Log.Error(err, "Failed to get Pod", "Pod.Name", podName)
			//return err
			continue
		}

		// 检查 Pod 是否健康
		if isPodHealthy(*pod) {
			log.Log.Info("pod就绪")
			r.Log.Info("Pod is healthy", "Pod.Name", podName)
			break
		} else {
			log.Log.Info("pod未就绪")
			r.Log.Info("Waiting for Pod to become healthy", "Pod.Name", podName)
		}
	}
	elapsed := time.Since(start)
	fmt.Printf("等待 pod 就绪耗时: %v\n", elapsed)

	return &pod, nil

}

// labelPod 为 Pod 打标签
func (r *YellowTangReconciler) labelPod(pod *corev1.Pod, role string, ctx context.Context, tang *appsv1.YellowTang) error {
	// 更新 Pod 标签
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels["role"] = role

	if err := r.Update(ctx, pod); err != nil {
		return fmt.Errorf("failed to update pod %s: %v", pod.Name, err)
	}

	return nil
}

// 制作主从同步的函数
func (r *YellowTangReconciler) setupMasterSlaveReplication(ctx context.Context, masterName string, slaveNames []string, tang *appsv1.YellowTang) error {
	log := log.FromContext(ctx)
	log.Info("setupMasterSlaveReplication函数", "masterName", masterName, "slaveNames", slaveNames)

	// 获取主库 Pod 对象
	masterPod := &corev1.Pod{}
	masterPodKey := client.ObjectKey{Namespace: tang.Namespace, Name: masterName}
	if err := r.Get(ctx, masterPodKey, masterPod); err != nil {
		return fmt.Errorf("failed to get master pod %s: %v", masterName, err)
	}

	// 打标签主库: 确保理解关联到主svc上，从库会通过主库的svc来连接进行同步
	if err := r.labelPod(masterPod, "master", ctx, tang); err != nil {
		return fmt.Errorf("failed to label master pod %s: %v", masterName, err)
	}

	// 为主库创建复制用户，并停止slave线程（如果之前自己是从库，那就应该停掉）
	masterCommand := fmt.Sprintf(
		"mysql -uroot -p%s -e \"CREATE USER IF NOT EXISTS 'replica'@'%%' IDENTIFIED BY 'password'; GRANT REPLICATION SLAVE ON *.* TO 'replica'@'%%';STOP slave;\"",
		MySQLPassword,
	)
	if _, err := r.execCommandOnPod(masterPod, masterCommand); err != nil {
		return fmt.Errorf("failed to execute command on master pod %s: %v", masterName, err)
	}

	// 配置每个从库: 如果从库名数组为空，则
	for _, slaveName := range slaveNames { // 如果没有从库，则循环结束，不会配置从库
		slavePod := &corev1.Pod{}
		slavePodKey := client.ObjectKey{Namespace: tang.Namespace, Name: slaveName}
		if err := r.Get(ctx, slavePodKey, slavePod); err != nil {
			return fmt.Errorf("failed to get slave pod %s: %v", slaveName, err)
		}

		// 配置主从复制: 先停slave，再配置、然后再启slave
		masterServiceName := tang.Spec.MasterServiceName
		slaveCommand := fmt.Sprintf(
			"mysql -uroot -p%s -e \"STOP SLAVE;CHANGE MASTER TO MASTER_HOST='%s', MASTER_USER='replica', MASTER_PASSWORD='password', MASTER_AUTO_POSITION=1; START SLAVE;\"",
			MySQLPassword,
			masterServiceName,
		)
		if _, err := r.execCommandOnPod(slavePod, slaveCommand); err != nil {
			return fmt.Errorf("failed to execute command on slave pod %s: %v", slaveName, err)
		}

		// 打标签
		if err := r.labelPod(slavePod, "slave", ctx, tang); err != nil {
			return fmt.Errorf("failed to label slave pod %s: %v", slaveName, err)
		}
	}

	return nil
}

// kubectl exec 进入pod内执行命令
func (r *YellowTangReconciler) execCommandOnPod(pod *corev1.Pod, command string) (string, error) {
	// Load kubeconfig from default location
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = "/root/.kube/config" // Fallback to default path
	}
	config, err := clientcmd.BuildConfigFromFlags("", KubeConfigPath) // 来自包："k8s.io/client-go/tools/clientcmd"
	// config, err := rest.InClusterConfig() // 来自包："k8s.io/client-go/rest"

	if err != nil {
		return "", err
	}

	// Create a new Kubernetes clientset
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", err
	}

	// Create REST client for pod exec
	restClient := kubeClient.CoreV1().RESTClient()
	req := restClient.
		Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		Param("stdin", "false").
		Param("stdout", "true").
		Param("stderr", "true").
		Param("tty", "false").
		Param("container", pod.Spec.Containers[0].Name).
		Param("command", "/bin/sh").
		Param("command", "-c").
		Param("command", command)

	// Create an executor
	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", err
	}

	// Execute the command
	var output strings.Builder
	err = executor.Stream(remotecommand.StreamOptions{
		Stdout: &output,
		Stderr: os.Stderr,
	})
	if err != nil {
		return "", err
	}

	return output.String(), nil
}
