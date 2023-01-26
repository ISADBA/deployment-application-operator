/*
Copyright 2023 fenghao.

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
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	dappsv1 "github.com/ISADBA/deployment-application-operator/api/v1"
	v1 "github.com/ISADBA/deployment-application-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const GenericRequeueDuration = 1 * time.Minute

var CounterReconcileApplication int

// ApplicationReconciler reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apps.isadba.com,resources=applications,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.isadba.com,resources=applications/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.isadba.com,resources=applications/finalizers,verbs=update
// 添加额外的role权限
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Application object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// add timer and counter
	<-time.NewTicker(1000 * time.Millisecond).C
	log := log.FromContext(ctx)

	CounterReconcileApplication += 1
	log.Info("Starting a reconcile", "number", CounterReconcileApplication)

	// get Application
	app := &v1.Application{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Application not found.")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get the Application, will requeue after a short time.")
		return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
	}

	// reconciler deployment
	var result ctrl.Result
	var err error

	result, err = r.reconcileDeployment(ctx, app)
	if err != nil {
		log.Error(err, "Failed to reconcile Deployment.")
		return result, err
	}

	result, err = r.reconcileService(ctx, app)
	if err != nil {
		log.Error(err, "Failed to reconcile Service.")
		return result, err
	}

	log.Info("All resources have been reconciled.")

	return ctrl.Result{}, nil

}

// reconcileDeployment logic
func (r *ApplicationReconciler) reconcileDeployment(ctx context.Context, app *v1.Application) (result ctrl.Result, err error) {
	fmt.Println("ReconcileDeployment ing......")
	log := log.FromContext(ctx)

	// get deployment
	var dp = &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: app.Namespace,
		Name:      app.Name,
	}, dp)

	// deployment status update
	if err == nil {
		log.Info("The Deployment has already exist.")

		if reflect.DeepEqual(dp.Spec, app.Spec.Deployment.DeploymentSpec) {
			if reflect.DeepEqual(dp.Status, app.Status.Workflow) {
				return ctrl.Result{}, nil
			}
			app.Status.Workflow = dp.Status
			if err := r.Status().Update(ctx, app); err != nil {
				log.Error(err, "Failed to update Application status")
				return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
			}
			log.Info("The Application status has been updated.")
			return ctrl.Result{}, nil
		}

		// update Deployment Spec
		dp.Spec = app.Spec.Deployment.DeploymentSpec
		dp.Spec.Template.SetLabels(app.Labels)
		if err = r.Update(ctx, dp); err != nil {
			log.Error(err, "Failed to update Deployment,will requeue after a short time.")
			return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
		}
		log.Info("Application.deployment with Deployment difference,Deployment will updated....")
		return ctrl.Result{}, nil
	}
	// Deployment Found, But have other error，next reconcile
	if !errors.IsNotFound(err) {
		log.Error(err, "Failed to get Deployment,will requeue after a short time.")
		return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
	}

	// Deployment Not Found, Create Deployment
	newDp := &appsv1.Deployment{}
	newDp.SetName(app.Name)
	newDp.SetNamespace(app.Namespace)
	newDp.SetLabels(app.Labels)
	newDp.Spec = app.Spec.Deployment.DeploymentSpec
	newDp.Spec.Template.SetLabels(app.Labels)

	// 设置deployment为application的子资源，当app删除后，deployment自动回收
	if err := ctrl.SetControllerReference(app, newDp, r.Scheme); err != nil {
		log.Error(err, "Failed to SetControllerReference,will requeue after a short time.")
		return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
	}

	if err := r.Create(ctx, newDp); err != nil {
		log.Error(err, "Failed to create Deployment,will requeue after a short time.")
		return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
	}

	log.Info("The Deployment has been created.")
	return ctrl.Result{}, nil
}

// reconcileService logic
func (r *ApplicationReconciler) reconcileService(ctx context.Context, app *v1.Application) (result ctrl.Result, err error) {
	fmt.Println("ReconcileService ing......")
	log := log.FromContext(ctx)

	var svc = &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: app.Namespace,
		Name:      app.Name,
	}, svc)

	if err == nil {
		log.Info("The Service has already exist.")
		if reflect.DeepEqual(svc.Status, app.Status.NetWork) {
			return ctrl.Result{}, nil
		}

		app.Status.NetWork = svc.Status

		if err := r.Status().Update(ctx, app); err != nil {
			log.Error(err, "Failed to update Application status")
			return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
		}

		log.Info("The Application status has been updated.")
		return ctrl.Result{}, nil
	}

	if !errors.IsNotFound(err) {
		log.Error(err, "Failed to get Service,will requeue after a short time.")
		return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
	}

	newSvc := &corev1.Service{}
	newSvc.SetName(app.Name)
	newSvc.SetNamespace(app.Namespace)
	newSvc.SetLabels(app.Labels)
	newSvc.Spec = app.Spec.Service.ServiceSpec
	newSvc.Spec.Selector = app.Labels

	// 设置service为application的子资源，当app删除后，service自动回收
	if err := ctrl.SetControllerReference(app, newSvc, r.Scheme); err != nil {
		log.Error(err, "Failed to SetControllerReference,will requeue after a short time.")
		return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
	}

	if err := r.Create(ctx, newSvc); err != nil {
		log.Error(err, "Failed to create Service,will requeue after a short time.")
		return ctrl.Result{RequeueAfter: GenericRequeueDuration}, err
	}

	log.Info("The Service has been created.")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	setupLog := ctrl.Log.WithName("setup")

	return ctrl.NewControllerManagedBy(mgr).
		For(&dappsv1.Application{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(event event.CreateEvent) bool {
				return true
			},
			DeleteFunc: func(event event.DeleteEvent) bool {
				setupLog.Info("The Application has been deleted.", "name", event.Object.GetName())
				return false
			},
			UpdateFunc: func(event event.UpdateEvent) bool {
				if event.ObjectNew.GetResourceVersion() == event.ObjectOld.GetResourceVersion() {
					return false
				}
				if reflect.DeepEqual(event.ObjectNew.(*v1.Application).Spec, event.ObjectOld.(*v1.Application).Spec) {
					return false
				}
				return true
			},
		})).Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.Funcs{
		CreateFunc: func(event event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(event event.DeleteEvent) bool {
			setupLog.Info("The deployment has been deleted.", "name", event.Object.GetName())
			return true
		},
		UpdateFunc: func(event event.UpdateEvent) bool {
			if event.ObjectNew.GetResourceVersion() == event.ObjectOld.GetResourceVersion() {
				setupLog.Info("The Deployment has been update,But ResourceVersion is same.", "ResourceVersion", event.ObjectOld.GetResourceVersion())
				return false
			}
			if reflect.DeepEqual(event.ObjectNew.(*appsv1.Deployment).Spec, event.ObjectOld.(*appsv1.Deployment).Spec) {
				setupLog.Info("The Deployment has been update,But Spec is same.")
				return false
			}
			setupLog.Info("The Deployment has been updated, will reconcile.......")
			return true
		},
		GenericFunc: nil,
	})).Owns(&corev1.Service{}, builder.WithPredicates(predicate.Funcs{
		CreateFunc: func(ce event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(de event.DeleteEvent) bool {
			setupLog.Info("The Service has been deleted.", "name", de.Object.GetName())
			return true
		},
		UpdateFunc: func(ue event.UpdateEvent) bool {
			if ue.ObjectNew.GetResourceVersion() == ue.ObjectOld.GetResourceVersion() {
				setupLog.Info("The Service has been update,But ResourceVersion is same.", "ResourceVersion", ue.ObjectOld.GetResourceVersion())
				return false
			}
			if reflect.DeepEqual(ue.ObjectNew.(*corev1.Service).Spec, ue.ObjectOld.(*corev1.Service).Spec) {
				setupLog.Info("The Service has been update,But Spec is same.")
				return false
			}
			setupLog.Info("The Service has been updated, will reconcile.......")
			return true
		},
	})).
		Complete(r)
}
