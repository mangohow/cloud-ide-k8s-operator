/*
Copyright 2023.

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

package controllers

import (
	"context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mv1 "github.com/mangohow/cloud-ide-k8s-operator/api/v1"
)

var Mode string

const (
	ModeRelease = "release"
	ModDev      = "dev"
)

// WorkSpaceReconciler reconciles a WorkSpace object
type WorkSpaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=cloud-ide.mangohow.com,resources=workspaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cloud-ide.mangohow.com,resources=workspaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cloud-ide.mangohow.com,resources=workspaces/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pod,verbs=get;list;watch;create;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the WorkSpace object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *WorkSpaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	//?????????WorkSpace
	wp := mv1.WorkSpace{}
	err := r.Client.Get(context.Background(), req.NamespacedName, &wp)
	// case 1???????????????Workspace,??????WorkSpace????????????,???????????????Pod???PVC??????
	if err != nil {
		if errors.IsNotFound(err) {
			if e1 := r.deletePod(req.NamespacedName); e1 != nil {
				klog.Errorf("[Delete Workspace] delete pod error:%v", e1)
				return ctrl.Result{Requeue: true}, e1
			}
			if e2 := r.deletePVC(req.NamespacedName); e2 != nil {
				klog.Errorf("[Delete Workspace] delete pvc error:%v", e2)
				return ctrl.Result{Requeue: true}, e2
			}

			return ctrl.Result{}, nil
		}

		klog.Errorf("get workspace error:%v", err)
		return ctrl.Result{Requeue: true}, err
	}

	// ?????????WorkSpace,??????WorkSpace???Operation??????????????????????????????
	switch wp.Spec.Operation {
	// case2: ??????WorkSpace,??????PVC????????????,????????????????????????
	case mv1.WorkSpaceStart:
		// ??????PVC????????????,??????????????????
		err = r.createPVC(&wp, req.NamespacedName)
		if err != nil {
			klog.Errorf("[Start Workspace] create pvc error:%v", err)
			return ctrl.Result{Requeue: true}, err
		}
		// ??????Pod
		err = r.createPod(&wp, req.NamespacedName)
		if err != nil {
			klog.Errorf("[Start Workspace] create pod error:%v", err)
			return ctrl.Result{Requeue: true}, err
		}
		r.updateStatus(&wp, mv1.WorkspacePhaseRunning)

	// case3: ??????WorkSpace,??????Pod
	case mv1.WorkSpaceStop:
		//??????Pod
		err = r.deletePod(req.NamespacedName)
		if err != nil {
			klog.Errorf("[Stop Workspace] delete pod error:%v", err)
			return ctrl.Result{Requeue: true}, err
		}

		r.updateStatus(&wp, mv1.WorkspacePhaseStopped)
	}

	return ctrl.Result{}, nil
}

func (r WorkSpaceReconciler) updateStatus(wp *mv1.WorkSpace, phase mv1.WorkSpacePhase) {
	wp.Status.Phase = phase
	err := r.Client.Status().Update(context.Background(), wp)
	if err != nil {
		klog.Errorf("update status error:%v", err)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkSpaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: 8}).
		For(&mv1.WorkSpace{}).
		Owns(&v1.Pod{}, builder.WithPredicates(predicatePod)).
		Owns(&v1.PersistentVolumeClaim{}, builder.WithPredicates(predicatePVC)).
		Complete(r)
}

func (r *WorkSpaceReconciler) createPod(space *mv1.WorkSpace, key client.ObjectKey) error {
	// 1.??????Pod????????????
	exist, err := r.checkPodExist(key)
	if err != nil {
		return err
	}

	// Pod?????????,????????????
	if exist {
		return nil
	}

	// 2.??????Pod
	pod := r.constructPod(space)

	// ??????????????????????????????????????????,????????????????????????????????????????????????????????????
	//if err = controllerutil.SetControllerReference(space, pod, r.Scheme); err != nil {
	//	return err
	//}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*30)
	defer cancelFunc()
	err = r.Client.Create(ctx, pod)
	if err != nil {
		// ??????Pod????????????,????????????
		if errors.IsAlreadyExists(err) {
			return nil
		}

		return err
	}

	return nil
}

// ????????????Pod??????
func (r *WorkSpaceReconciler) constructPod(space *mv1.WorkSpace) *v1.Pod {
	volumeName := "volume-user-workspace"
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      space.Name,
			Namespace: space.Namespace,
			Labels: map[string]string{
				"app": "cloud-ide",
			},
		},
		Spec: v1.PodSpec{
			Volumes: []v1.Volume{
				{
					Name: volumeName,
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: space.Name,
							ReadOnly:  false,
						},
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:            space.Name,
					Image:           space.Spec.Image,
					ImagePullPolicy: v1.PullIfNotPresent,
					Ports: []v1.ContainerPort{
						{
							ContainerPort: space.Spec.Port,
						},
					},
					// ?????????????????????
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      volumeName,
							ReadOnly:  false,
							MountPath: space.Spec.MountPath,
						},
					},
				},
			},
		},
	}

	if Mode == ModeRelease {
		// ????????????CPU2????????????1Gi == 1 * 2^10
		pod.Spec.Containers[0].Resources = v1.ResourceRequirements{
			Requests: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse(space.Spec.Cpu),
				v1.ResourceMemory: resource.MustParse(space.Spec.Memory),
			},
		}
	}

	return pod
}

func (r *WorkSpaceReconciler) createPVC(space *mv1.WorkSpace, key client.ObjectKey) error {
	// 1.?????????PVC??????????????????
	exist, err := r.checkPVCExist(key)
	if err != nil {
		// PVC????????????
		return err
	}

	// PVC????????????,????????????
	if exist {
		return nil
	}

	// 2.PVC?????????,??????PVC
	pvc, err := r.constructPVC(space)
	if err != nil {
		klog.Errorf("construct pvc error:%v", err)
		return err
	}

	// ?????????OwnerReference??????,PVC?????????????????????,????????????Reconcile??????
	// ????????????PVC??????,????????????????????????????????????,????????????????????????????????????????????????
	//if err = controllerutil.SetControllerReference(space, pvc, r.Scheme); err != nil {
	//	return err
	//}

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*30)
	defer cancelFunc()
	err = r.Client.Create(ctx, pvc)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}

		return err
	}

	return nil
}

// ??????PVC??????
func (r *WorkSpaceReconciler) constructPVC(space *mv1.WorkSpace) (*v1.PersistentVolumeClaim, error) {
	quantity, err := resource.ParseQuantity(space.Spec.Storage)
	if err != nil {
		return nil, err
	}

	pvc := &v1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      space.Name,
			Namespace: space.Namespace,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteMany},
			Resources: v1.ResourceRequirements{
				Limits:   v1.ResourceList{v1.ResourceStorage: quantity},
				Requests: v1.ResourceList{v1.ResourceStorage: quantity},
			},
		},
	}

	return pvc, nil
}

func (r *WorkSpaceReconciler) checkPodExist(key client.ObjectKey) (bool, error) {
	pod := &v1.Pod{}
	// ???????????????
	err := r.Client.Get(context.Background(), key, pod)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}

		klog.Errorf("get pod error:%v", err)
		return false, err
	}

	return true, nil
}

func (r *WorkSpaceReconciler) deletePod(key client.ObjectKey) error {
	exist, err := r.checkPodExist(key)
	if err != nil {
		return err
	}

	// Pod?????????,????????????
	if !exist {
		return nil
	}

	pod := &v1.Pod{}
	pod.Name = key.Name
	pod.Namespace = key.Namespace

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*35)
	defer cancelFunc()
	// ??????Pod
	err = r.Client.Delete(ctx, pod)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}

		klog.Errorf("delete pod error:%v", err)
		return err
	}

	return nil
}

func (r *WorkSpaceReconciler) checkPVCExist(key client.ObjectKey) (bool, error) {
	pvc := &v1.PersistentVolumeClaim{}
	err := r.Client.Get(context.Background(), key, pvc)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}

		klog.Errorf("get pvc error:%v", err)
		return false, err
	}

	return true, nil
}

func (r *WorkSpaceReconciler) deletePVC(key client.ObjectKey) error {
	exist, err := r.checkPVCExist(key)
	if err != nil {
		return err
	}

	// pvc?????????,???????????????
	if !exist {
		return nil
	}

	pvc := &v1.PersistentVolumeClaim{}
	pvc.Name = key.Name
	pvc.Namespace = key.Namespace

	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*30)
	defer cancelFunc()
	err = r.Client.Delete(ctx, pvc)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}

		klog.Errorf("delete pvc error:%v", err)
		return err
	}

	return nil
}
