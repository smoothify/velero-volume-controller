/*
Copyright 2017 The Kubernetes Authors.

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

package velerovolume

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ryanuber/go-glob"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"github.com/smoothify/velero-volume-controller/cmd/controller/velerovolume/config"
	"github.com/smoothify/velero-volume-controller/pkg/constants"
	"github.com/smoothify/velero-volume-controller/pkg/helpers"
)

// Controller is the controller implementation for Pod resources
type Controller struct {
	cfg *config.VeleroVolumeCfg
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface

	podsLister corelisters.PodLister
	podsSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
}

// NewController returns a new sample controller
func NewController(
	cfg *config.VeleroVolumeCfg,
	kubeclientset kubernetes.Interface,
	podInformer coreinformers.PodInformer) *Controller {

	controller := &Controller{
		cfg:           cfg,
		kubeclientset: kubeclientset,
		podsLister:    podInformer.Lister(),
		podsSynced:    podInformer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Pods"),
	}

	klog.Info("Setting up event handlers")
	// Set up an event handler for when Pod resources change
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			klog.V(4).Infof("Watch Pod: '%s/%s' Added ...", pod.Namespace, pod.Name)
			controller.enqueuePod(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			newPod := new.(*corev1.Pod)
			oldPod := old.(*corev1.Pod)
			klog.V(4).Infof("Watch Pod: '%s/%s' Updated ...", newPod.Namespace, newPod.Name)
			if newPod.ResourceVersion == oldPod.ResourceVersion {
				// Periodic resync will send update events for all known Pods.
				// Two different versions of the same Pod will always have different RVs.
				return
			}
			controller.enqueuePod(new)
		},
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting VeleroVolume controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.podsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	// Launch one worker to process Pod resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Pod resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then detects and adds relevant backup annotations to pods with volumes.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the Pod resource with this namespace/name
	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		// The Pod resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("pod '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	// Check whether this pod satisfies the filter options, and if it
	// doesn't, try to remove the backup annotation if present
	if !c.checkFilterOptions(pod) {
		err = c.removeBackupAnnotationsFromPod(pod)
		if err != nil {
			return err
		}
		return nil
	}

	if pod.Status.Phase == corev1.PodRunning {
		// Try to add restic backup annotation to pod when it is running
		err = c.addBackupAnnotationsToPod(pod)
		if err != nil {
			klog.Errorf("failed to add velero restic backup annotation to pod: '%s/%s', error: %s", pod.Namespace, pod.Name, err.Error())
			return err
		}
	} else {
		// Try to remove restic backup annotation from pod when it is not running anymore
		err = c.removeBackupAnnotationsFromPod(pod)
		if err != nil {
			return err
		}
	}

	return nil
}

// checkFilterOptions aims to bypass pods that don't meet filter requirements
func (c *Controller) checkFilterOptions(pod *corev1.Pod) bool {
	// If pod is pending, we ignore it for moment
	if pod.Status.Phase == corev1.PodPending {
		return false
	}

	// Drop pods controlled by excluding jobs
	if c.cfg.ExcludeJobs != "" {
		for _, owner := range pod.OwnerReferences {
			if owner.Kind == "Job" {
				excludeJobs := strings.Split(c.cfg.ExcludeJobs, ",")
				for _, job := range excludeJobs {
					if glob.Glob(job, owner.Name) {
						klog.V(4).Infof("drop pod: '%s/%s' as it's controlled by a job within the range of excluding jobs", pod.Namespace, pod.Name)
						return false
					}
				}
			}
		}
	}

	// Drop pods that don't meet namespace requirements
	if c.cfg.IncludeNamespaces != "" {
		flag := false
		includeNamespaces := strings.Split(c.cfg.IncludeNamespaces, ",")
		for _, namespace := range includeNamespaces {
			if namespace == pod.Namespace {
				flag = true
				break
			}
		}
		if !flag {
			klog.V(4).Infof("drop pod: '%s/%s' as it's outside the range of including namespaces", pod.Namespace, pod.Name)
			return false
		}
	} else if c.cfg.ExcludeNamespaces != "" {
		flag := true
		excludeNamespaces := strings.Split(c.cfg.ExcludeNamespaces, ",")
		for _, namespace := range excludeNamespaces {
			if namespace == pod.Namespace {
				flag = false
				break
			}
		}
		if !flag {
			klog.V(4).Infof("drop pod: '%s/%s' as it's within the range of excluding namespaces", pod.Namespace, pod.Name)
			return false
		}
	}

	// The pod passed all the checks
	return true
}

// addBackupAnnotationsToPod adds relevant backup annotation to pod with volumes.
// The logic of AddBackupAnnotationsToPod is kept as simple as possible, which will
// iterate over all volumes of the pod and add the velero backup annotation to it.
func (c *Controller) addBackupAnnotationsToPod(pod *corev1.Pod) error {
	// Iterate over all volumes
	var veleroBackupAnnotationArray []string

	for _, volume := range pod.Spec.Volumes {
		volumeType := helpers.GetPodVolumeType(volume.VolumeSource)

		// Check if volume uses persistentVolumeClaim and if so, retrieve underlying volume
		if volume.PersistentVolumeClaim != nil {
			// Check if claim name meets requirements
			if !c.checkVolumeClaimNameRequirements(pod.Namespace, volume.PersistentVolumeClaim.ClaimName) {
				break
			}

			klog.V(4).Infof("pod '%s/%s' uses volume '%s' from pvc '%s'", pod.Namespace, pod.Name, volume.Name, volume.PersistentVolumeClaim.ClaimName)

			claim, err := c.kubeclientset.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(context.TODO(), volume.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			pv, err := c.kubeclientset.CoreV1().PersistentVolumes().Get(context.TODO(), claim.Spec.VolumeName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			klog.V(4).Infof("volume %s retrieved for claim %s", pv.Name, claim.Name)

			// Check if the volume or claim is excluded via an annotation
			if helpers.Truthy(claim.Annotations[constants.VOLUME_BACKUP_EXCLUDE_ANNOTATION_KEY]) ||
				helpers.Truthy(pv.Annotations[constants.VOLUME_BACKUP_EXCLUDE_ANNOTATION_KEY]) {
				klog.V(4).Infof("volume %s of type %s has explicit exclude annotation", volume.Name, volumeType)
				break
			}

			// Check if the volume or claim is included via an annotation
			if !helpers.Truthy(claim.Annotations[constants.VOLUME_BACKUP_INCLUDE_ANNOTATION_KEY]) &&
				!helpers.Truthy(pv.Annotations[constants.VOLUME_BACKUP_INCLUDE_ANNOTATION_KEY]) {
				klog.V(4).Infof("volume %s of type %s has explicit include annotation", volume.Name, volumeType)
				veleroBackupAnnotationArray = append(veleroBackupAnnotationArray, volume.Name)
			}

			// Check if persistent volume name meets requirements
			if !c.checkVolumeNameRequirements(pv.Name) {
				break
			}

			storageClass := *claim.Spec.StorageClassName
			if storageClass == "" {
				storageClass = pv.Spec.StorageClassName
			}
			if !c.checkVolumeStorageClassRequirements(storageClass) {
				break
			}
			volumeType = helpers.GetPersistentVolumeType(pv.Spec.PersistentVolumeSource)
		}

		// Check if volume meets volume type requirements
		if c.checkVolumeTypeRequirements(volumeType, volume.PersistentVolumeClaim != nil) {
			klog.V(4).Infof("volume %s of type %s matches requirements", volume.Name, volumeType)
			veleroBackupAnnotationArray = append(veleroBackupAnnotationArray, volume.Name)
		}

	}
	if len(veleroBackupAnnotationArray) > 0 {
		// NEVER modify objects from the store. It's a read-only, local cache.
		// You can use DeepCopy() to make a deep copy of original object and modify this copy
		// Or create a copy manually for better performance
		veleroBackupAnnotationValue := strings.Join(veleroBackupAnnotationArray, ",")
		podCopy := pod.DeepCopy()
		if podCopy.Annotations != nil {
			podCopy.Annotations[constants.VELERO_BACKUP_ANNOTATION_KEY] = veleroBackupAnnotationValue
		} else {
			podCopy.Annotations = map[string]string{
				constants.VELERO_BACKUP_ANNOTATION_KEY: veleroBackupAnnotationValue,
			}
		}
		// Update pod annotations
		_, err := c.kubeclientset.CoreV1().Pods(pod.Namespace).Update(context.TODO(), podCopy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		klog.V(4).Infof("add velero restic backup annotation: '%s=%s' to pod '%s/%s' successfully", constants.VELERO_BACKUP_ANNOTATION_KEY, veleroBackupAnnotationValue, pod.Namespace, pod.Name)
	} else {
		// No volumes to backup anymore, try to remove backup annotation if present
		err := c.removeBackupAnnotationsFromPod(pod)
		if err != nil {
			return err
		}
	}
	return nil
}

// removeBackupAnnotationsFromPod trys to remove relevant backup annotation from pod.
// This function aims to avoid the following situations:
// 1. restic backup PartiallyFailed when pod status changed.
// Refs to https://github.com/duyanghao/velero-volume-controller/issues/6
// 2. restic backup PartiallyFailed when pod volume disappeared.
// 3. restic continues to backup pods that can't meet filter options anymore.
// Refs to https://github.com/duyanghao/velero-volume-controller/pull/11
func (c *Controller) removeBackupAnnotationsFromPod(pod *corev1.Pod) error {
	if pod.Annotations != nil {
		if _, exist := pod.Annotations[constants.VELERO_BACKUP_ANNOTATION_KEY]; exist {
			// Remove annotation as it is present
			podCopy := pod.DeepCopy()
			delete(podCopy.Annotations, constants.VELERO_BACKUP_ANNOTATION_KEY)

			// Update pod annotations
			_, err := c.kubeclientset.CoreV1().Pods(pod.Namespace).Update(context.TODO(), podCopy, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("failed to remove velero restic backup annotation from pod: '%s/%s', error: %s", pod.Namespace, pod.Name, err.Error())
				return err
			}
			klog.V(4).Infof("remove velero restic backup annotation: '%s' from pod '%s/%s' successfully", constants.VELERO_BACKUP_ANNOTATION_KEY, pod.Namespace, pod.Name)
		}
	}
	return nil
}

// checkVolumeTypeRequirements is a function that indicates if a volume meets backup volume type requirements
func (c *Controller) checkVolumeTypeRequirements(volumeType string, isClaim bool) bool {
	if c.cfg.IncludeVolumeTypes != "" {
		includeVolumeTypes := strings.Split(strings.ReplaceAll(c.cfg.IncludeVolumeTypes, " ", ""), ",")
		for _, vt := range includeVolumeTypes {
			if strings.EqualFold(vt, volumeType) {
				return true
			} else if isClaim && vt == constants.VOLUME_TYPE_PERSISTENTVOLUMECLAIM {
				return true
			}
		}
		return false
	} else {
		excludeVolumeTypes := strings.Split(strings.ReplaceAll(c.cfg.ExcludeVolumeTypes, " ", ""), ",")
		excludeVolumeTypes = append(excludeVolumeTypes, "hostPath", "secret", "configMap", "emptyDir")
		for _, vt := range excludeVolumeTypes {
			if strings.EqualFold(vt, volumeType) {
				return false
			} else if isClaim && vt == constants.VOLUME_TYPE_PERSISTENTVOLUMECLAIM {
				return false
			}
		}
	}
	return true
}

// checkVolumeClaimNameRequirements is a function that indicates if a claim meets backup claim name requirements
func (c *Controller) checkVolumeClaimNameRequirements(namespace string, claimName string) bool {
	if c.cfg.IncludeClaimNames != "" {
		includeNames := strings.Split(strings.ReplaceAll(c.cfg.IncludeClaimNames, " ", ""), ",")
		for _, cn := range includeNames {
			// Include name has a namespace specified
			if strings.Contains(cn, "/") {
				if cn == fmt.Sprintf("%s/%s", namespace, claimName) {
					return true
				}
			} else {
				if cn == claimName {
					return true
				}
			}
		}
		return false
	} else if c.cfg.ExcludeClaimNames != "" {
		excludeNames := strings.Split(strings.ReplaceAll(c.cfg.ExcludeClaimNames, " ", ""), ",")
		for _, cn := range excludeNames {
			// Exclude name has a namespace specified
			if strings.Contains(cn, "/") {
				if cn == fmt.Sprintf("%s/%s", namespace, claimName) {
					return true
				}
			} else {
				if cn == claimName {
					return true
				}
			}
		}
	}
	return true
}

// checkVolumeNameRequirements is a function that indicates if a volume meets backup volume name requirements
func (c *Controller) checkVolumeNameRequirements(volumeName string) bool {
	if c.cfg.IncludeVolumeNames != "" {
		includeNames := strings.Split(strings.ReplaceAll(c.cfg.IncludeVolumeNames," ", ""), ",")
		for _, cn := range includeNames {
			if cn == volumeName {
				return true
			}
		}
		return false
	} else if c.cfg.ExcludeVolumeNames != "" {
		excludeNames := strings.Split(strings.ReplaceAll(c.cfg.ExcludeVolumeNames, " ", ""),",")
		for _, cn := range excludeNames {
			if cn == volumeName {
				return false
			}
		}
	}
	return true
}

// checkVolumeStorageClassRequirements is a function that indicates if a volume meets storage class requirements
func (c *Controller) checkVolumeStorageClassRequirements(storageClass string) bool {
	if c.cfg.IncludeStorageClasses != "" {
		includeClasses := strings.Split(strings.ReplaceAll(c.cfg.IncludeStorageClasses, " ", ""), ",")
		for _, sc := range includeClasses {
			if sc == storageClass {
				return true
			}
		}
		return false
	} else if c.cfg.ExcludeStorageClasses != "" {
		excludeClasses := strings.Split(strings.ReplaceAll(c.cfg.ExcludeStorageClasses, " ", ""), ",")
		for _, sc := range excludeClasses {
			if sc == storageClass {
				return false
			}
		}
	}
	return true
}

// enqueuePod takes a Pod resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Pod.
func (c *Controller) enqueuePod(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}
