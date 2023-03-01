package service

import (
	"context"
	"fmt"
	mv1 "github.com/mangohow/cloud-ide-k8s-operator/api/v1"
	"github.com/mangohow/cloud-ide-k8s-operator/pb"
	"google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"
)

const (
	PodNotExist int32 = iota
	PodExist
)

type WorkSpaceService struct {
	client client.Client
}

func NewWorkSpaceService(c client.Client) *WorkSpaceService {
	return &WorkSpaceService{
		client: c,
	}
}

var _ = pb.CloudIdeServiceServer(&WorkSpaceService{})

var (
	WorkspaceAlreadyExist = "workspace already exist"
	WorkspaceCreateFailed = "create workspace error"
	WorkspaceNotExist     = "workspace not exist"
	WorkspaceStartFailed  = "start workspace error"
	WorkspaceDeleteFailed = "delete workspace error"
)

var (
	EmptyWorkspaceRunningInfo = &pb.WorkspaceRunningInfo{}
	EmptyResponse             = &pb.Response{}
	EmptyWorkspaceInfo        = &pb.WorkspaceInfo{}
	EmptyWorkspaceStatus      = &pb.WorkspaceStatus{}
)

// CreateSpace 创建并且启动Workspace,将Operation字段置为"Start",当Workspace被创建时,PVC和Pod也会被创建
func (s *WorkSpaceService) CreateSpace(ctx context.Context, info *pb.WorkspaceInfo) (*pb.WorkspaceRunningInfo, error) {
	// 1.先查询workspace是否存在
	var wp mv1.WorkSpace
	exist := s.checkWorkspaceExist(ctx, client.ObjectKey{Name: info.Name, Namespace: info.Namespace}, &wp)
	stus := status.New(codes.AlreadyExists, WorkspaceAlreadyExist)
	if exist {
		return EmptyWorkspaceRunningInfo, stus.Err()
	}

	// 2.如果不存在就创建
	w := s.constructWorkspace(info)
	if err := s.client.Create(ctx, w); err != nil {
		if errors.IsAlreadyExists(err) {
			return EmptyWorkspaceRunningInfo, stus.Err()
		}

		klog.Errorf("create workspace error:%v", err)
		return EmptyWorkspaceRunningInfo, status.Error(codes.Unknown, err.Error())
	}

	// 3.等待Pod处于Running状态
	return s.waitForPodRunning(ctx, client.ObjectKey{Name: w.Name, Namespace: w.Namespace}, w)
}

func (s *WorkSpaceService) waitForPodRunning(ctx context.Context, key client.ObjectKey, wp *mv1.WorkSpace) (*pb.WorkspaceRunningInfo, error) {
	// 获取Pod的运行信息,可能会因为资源不足而导致Pod无法运行
	// 最多重试四次,如果还不行,就停止工作空间
	retry, maxRetry := 0, 5
	sleepDuration := []time.Duration{1, 3, 5, 8, 12}
	po := v1.Pod{}

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		default:
			if retry >= maxRetry {
				break loop
			}

			// 先休眠,等待Pod被创建并且运行起来
			time.Sleep(time.Second * sleepDuration[retry])

			if err := s.client.Get(context.Background(), key, &po); err != nil {
				if !errors.IsNotFound(err) {
					klog.Errorf("get pod error:%v", err)
				}
			} else {
				if po.Status.Phase == v1.PodRunning {
					return &pb.WorkspaceRunningInfo{
						NodeName: po.Spec.NodeName,
						Ip:       po.Status.PodIP,
						Port:     po.Spec.Containers[0].Ports[0].ContainerPort,
					}, nil
				}
			}

			retry++
		}
	}

	// 5.处理错误情况,停止工作空间
	s.StopSpace(ctx, &pb.QueryOption{
		Name:      wp.Name,
		Namespace: wp.Namespace,
	})

	return EmptyWorkspaceRunningInfo, status.Error(codes.ResourceExhausted, WorkspaceStartFailed)
}

// StartSpace 启动Workspace
func (s *WorkSpaceService) StartSpace(ctx context.Context, info *pb.WorkspaceInfo) (*pb.WorkspaceRunningInfo, error) {
	// 1.先获取workspace,如果不存在返回错误
	var wp mv1.WorkSpace
	key := client.ObjectKey{Name: info.Name, Namespace: info.Namespace}
	exist := s.checkWorkspaceExist(ctx, key, &wp)
	if !exist {
		return EmptyWorkspaceRunningInfo, status.Error(codes.NotFound, WorkspaceNotExist)
	}

	// 2.查询Pod是否存在,如果存在直接返回数据
	pod := v1.Pod{}
	if err := s.client.Get(context.Background(), key, &pod); err == nil {
		return &pb.WorkspaceRunningInfo{
			NodeName: pod.Spec.NodeName,
			Ip:       pod.Status.PodIP,
			Port:     pod.Spec.Containers[0].Ports[0].ContainerPort,
		}, nil
	}

	// 3.更新Workspace,使用RetryOnConflict,当资源版本冲突时重试
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// 每次更新前要获取最新的版本
		var p mv1.WorkSpace
		exist := s.checkWorkspaceExist(ctx, key, &p)
		if !exist {
			return nil
		}

		// 更新workspace的Operation字段
		wp.Spec.Operation = mv1.WorkSpaceStart
		if err := s.client.Update(ctx, &wp); err != nil {
			klog.Errorf("update workspace to start error:%v", err)
			return err
		}

		return nil
	})

	if err != nil {
		return EmptyWorkspaceRunningInfo, status.Error(codes.Unknown, err.Error())
	}

	if !exist {
		return EmptyWorkspaceRunningInfo, status.Error(codes.NotFound, WorkspaceNotExist)
	}

	return s.waitForPodRunning(ctx, key, &wp)
}

// DeleteSpace 只需要将workspace删除即可,controller会负责删除对应的Pod和PVC
func (s *WorkSpaceService) DeleteSpace(ctx context.Context, option *pb.QueryOption) (*pb.Response, error) {
	// 先查询是否存在,如果不存在则也认为成功
	var wp mv1.WorkSpace
	exist := s.checkWorkspaceExist(ctx, client.ObjectKey{Name: option.Name, Namespace: option.Namespace}, &wp)
	if !exist {
		return EmptyResponse, nil
	}

	// 删除Workspace
	if err := s.client.Delete(ctx, &wp); err != nil {
		klog.Errorf("delete workspace error:%v", err)
		return EmptyResponse, status.Error(codes.Unknown, err.Error())
	}

	return EmptyResponse, nil
}

// StopSpace 停止Workspace,只需要删除对应的Pod,因此修改Workspace的操作为Stop即可
func (s *WorkSpaceService) StopSpace(ctx context.Context, option *pb.QueryOption) (*pb.Response, error) {
	// 使用Update时,可能由于版本冲突而导致失败,需要重试
	exist := true
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var wp mv1.WorkSpace
		exist = s.checkWorkspaceExist(ctx, client.ObjectKey{Name: option.Name, Namespace: option.Namespace}, &wp)
		if !exist {
			return nil
		}

		// 更新workspace的Operation字段
		wp.Spec.Operation = mv1.WorkSpaceStop
		if err := s.client.Update(ctx, &wp); err != nil {
			klog.Errorf("update workspace to start error:%v", err)
			return err
		}

		return nil
	})

	if err != nil {
		return EmptyResponse, status.Error(codes.Unknown, err.Error())
	}

	if !exist {
		return EmptyResponse, status.Error(codes.NotFound, WorkspaceNotExist)
	}

	return EmptyResponse, nil
}

func (s *WorkSpaceService) GetPodSpaceStatus(ctx context.Context, option *pb.QueryOption) (*pb.WorkspaceStatus, error) {
	pod := v1.Pod{}
	err := s.client.Get(ctx, client.ObjectKey{Name: option.Name, Namespace: option.Namespace}, &pod)
	if err != nil {
		if errors.IsNotFound(err) {
			return EmptyWorkspaceStatus, status.Error(codes.NotFound, "workspace is not running")
		}

		klog.Errorf("get pod space status error:%v", err)
		return &pb.WorkspaceStatus{Status: PodNotExist, Message: "NotExist"}, status.Error(codes.NotFound, err.Error())
	}

	return &pb.WorkspaceStatus{Status: PodExist, Message: string(pod.Status.Phase)}, nil
}

func (s *WorkSpaceService) GetPodSpaceInfo(ctx context.Context, option *pb.QueryOption) (*pb.WorkspaceRunningInfo, error) {
	pod := v1.Pod{}
	err := s.client.Get(ctx, client.ObjectKey{Name: option.Name, Namespace: option.Namespace}, &pod)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "workspace is not running")
		}

		klog.Errorf("get pod space info error:%v", err)
		return EmptyWorkspaceRunningInfo, status.Error(codes.Unknown, err.Error())
	}

	return &pb.WorkspaceRunningInfo{
		NodeName: pod.Spec.NodeName,
		Ip:       pod.Status.PodIP,
		Port:     pod.Spec.Containers[0].Ports[0].ContainerPort,
	}, nil
}

func (s *WorkSpaceService) checkWorkspaceExist(ctx context.Context, key client.ObjectKey, w *mv1.WorkSpace) bool {
	if err := s.client.Get(ctx, key, w); err != nil {
		if errors.IsNotFound(err) {
			return false
		}

		klog.Errorf("get workspace error:%v", err)
		return false
	}

	return true
}

func (s *WorkSpaceService) constructWorkspace(space *pb.WorkspaceInfo) *mv1.WorkSpace {
	hardware := fmt.Sprintf("%sC%s%s", space.ResourceLimit.Cpu,
		strings.Split(space.ResourceLimit.Memory, "i")[0], strings.Split(space.ResourceLimit.Storage, "i")[0])
	return &mv1.WorkSpace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cloud-ide.mangohow.com/v1",
			Kind:       "WorkSpace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      space.Name,
			Namespace: space.Namespace,
		},
		Spec: mv1.WorkSpaceSpec{
			Cpu:       space.ResourceLimit.Cpu,
			Memory:    space.ResourceLimit.Memory,
			Storage:   space.ResourceLimit.Storage,
			Hardware:  hardware,
			Image:     space.Image,
			Port:      space.Port,
			MountPath: space.VolumeMountPath,
			Operation: mv1.WorkSpaceStart,
		},
	}
}
