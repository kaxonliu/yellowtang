package controller

import (
	"context"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "yellowtang/api/v1"
)

func TrimLinesRegex(text string) string {
	// 去除每行开头的空格和制表符
	re := regexp.MustCompile(`(?m)^[ \t]+|[ \t]+$`)
	return re.ReplaceAllString(text, "")
}

func (r *YellowTangReconciler) getService(serviceKey client.ObjectKey, ctx context.Context, tang *appsv1.YellowTang) (*corev1.Service, error) {
	service := corev1.Service{}
	if err := r.Get(ctx, serviceKey, &service); err != nil {
		return nil, err
	}
	return &service, nil

}

func (r *YellowTangReconciler) getorCreateService(name string, role string, ctx context.Context, tang *appsv1.YellowTang) (*corev1.Service, error) {
	serviceKey := client.ObjectKey{Namespace: tang.Spec.NameSpace, Name: name}
	service, err := r.getService(serviceKey, ctx, tang)
	if err == nil {
		return service, nil
	}

	if errors.IsNotFound(err) {
		service, err := r.createService(name, role, ctx, tang)
		if err == nil {
			return service, nil
		}
	}
	return nil, err
}

func (r *YellowTangReconciler) createService(name string, role string, ctx context.Context, tang *appsv1.YellowTang) (*corev1.Service, error) {
	// 定义 OwnerReference
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
			Namespace: tang.Spec.NameSpace,
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
	cmKey := client.ObjectKey{Namespace: tang.Spec.NameSpace, Name: name}
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
			Namespace: tang.Spec.NameSpace,
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
	pvcKey := client.ObjectKey{Namespace: tang.Spec.NameSpace, Name: name}
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
			Namespace: tang.Spec.NameSpace,
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
			Namespace: tang.Spec.NameSpace,
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
	return &pod, nil

}
